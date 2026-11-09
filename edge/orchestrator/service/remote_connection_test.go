package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/link"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// ── fakes ─────────────────────────────────────────────────────────────────────

// fakeCloud is the cloud's connection API: it stores what is created, so a
// second list sees it, like the real thing.
type fakeCloud struct {
	mu        sync.Mutex
	conns     []types.Connection
	listErr   error // returned by ListConnections while set
	createErr error
	lists     int
	creates   int
	nextID    int
	queries   []types.QueryConnectionsRequest

	// The real cloud refuses a connection for a node it was never told about
	// ("Node not found", 404); known is the node set the last SyncNodes left.
	known   map[string]bool
	syncErr error // returned by SyncNodes while set
	syncs   int
	synced  [][]string // node ids of each sync, in order
}

func (f *fakeCloud) SyncNodes(_ context.Context, nodes []types.Node) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.syncs++
	if f.syncErr != nil {
		return f.syncErr
	}
	f.known = map[string]bool{}
	var ids []string
	for _, n := range nodes {
		f.known[n.ID] = true
		ids = append(ids, n.ID)
	}
	f.synced = append(f.synced, ids)
	return nil
}

func (f *fakeCloud) ListConnections(_ context.Context, q types.QueryConnectionsRequest) ([]types.Connection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	f.queries = append(f.queries, q)
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []types.Connection
	for _, c := range f.conns {
		// Deliberately sloppy, like a cloud that filters loosely: the ensurer
		// must not rely on the filter being exact.
		if q.NodeID != nil && string(c.NodeID) != *q.NodeID {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeCloud) CreateConnection(_ context.Context, r types.CreateConnectionRequest) (*types.Connection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	if f.createErr != nil {
		return nil, f.createErr
	}
	if !f.known[r.NodeID] {
		return nil, errors.New(`status 404: {"detail":"Node not found"}`)
	}
	f.nextID++
	c := types.Connection{ID: fmt.Sprintf("conn_new%d", f.nextID), NodeID: types.ForeignKey(r.NodeID), Name: r.Name, State: types.ConnectionStateProvisioned}
	f.conns = append(f.conns, c)
	return &c, nil
}

func (f *fakeCloud) counts() (lists, creates int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lists, f.creates
}

func (f *fakeCloud) set(fn func(*fakeCloud)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// fakeRemoteAgents is an AgentDirectory whose agents record SetRemoteConnection.
type fakeRemoteAgents struct {
	mu     sync.Mutex
	linked map[string]bool
	sent   map[string][]string // node -> connection ids received, in order
	err    error               // returned by SetRemoteConnection while set
}

func newFakeRemoteAgents(linked ...string) *fakeRemoteAgents {
	f := &fakeRemoteAgents{linked: map[string]bool{}, sent: map[string][]string{}}
	for _, id := range linked {
		f.linked[id] = true
	}
	return f
}

func (f *fakeRemoteAgents) LinkedNodes() []string { return nil }
func (f *fakeRemoteAgents) isLinked(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.linked[id]
}
func (f *fakeRemoteAgents) setLinked(id string, up bool) {
	f.mu.Lock()
	f.linked[id] = up
	f.mu.Unlock()
}
func (f *fakeRemoteAgents) setErr(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}
func (f *fakeRemoteAgents) received(id string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent[id]...)
}
func (f *fakeRemoteAgents) Agent(id string) clients.AgentAPI { return &remoteAgent{f: f, id: id} }

type remoteAgent struct {
	clients.AgentAPI // everything else is unused here and would panic
	f                *fakeRemoteAgents
	id               string
}

func (a *remoteAgent) SetRemoteConnection(_ context.Context, connID string) error {
	a.f.mu.Lock()
	defer a.f.mu.Unlock()
	if !a.f.linked[a.id] {
		return &clients.NoLinkError{NodeID: a.id}
	}
	if a.f.err != nil {
		return a.f.err
	}
	a.f.sent[a.id] = append(a.f.sent[a.id], connID)
	return nil
}

type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) logf(format string, args ...any) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}
func (l *logSink) count(sub string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, s := range l.lines {
		if strings.Contains(s, sub) {
			n++
		}
	}
	return n
}

type ensureEnv struct {
	cloud  *fakeCloud
	agents *fakeRemoteAgents
	logs   *logSink
	e      *RemoteConnectionEnsurer

	mu         sync.Mutex
	registered bool
	enabled    bool
}

func (v *ensureEnv) setRegistered(b bool) { v.mu.Lock(); v.registered = b; v.mu.Unlock() }

func newEnsureEnv(t *testing.T, linked ...string) *ensureEnv {
	t.Helper()
	v := &ensureEnv{cloud: &fakeCloud{}, agents: newFakeRemoteAgents(linked...), logs: &logSink{}, registered: true, enabled: true}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	v.e = NewRemoteConnectionEnsurer(ctx, RemoteConnectionConfig{
		Enabled: func() bool { v.mu.Lock(); defer v.mu.Unlock(); return v.enabled },
		Cloud: func() (CloudConnections, error) {
			v.mu.Lock()
			defer v.mu.Unlock()
			if !v.registered {
				return nil, ErrNotRegistered
			}
			return v.cloud, nil
		},
		Nodes: func() ([]types.Node, error) {
			var nodes []types.Node
			for _, id := range linked {
				nodes = append(nodes, types.Node{ID: id, Name: id})
			}
			return nodes, nil
		},
		Agents: v.agents,
		Linked: v.agents.isLinked,
		Logf:   v.logs.logf,

		MinBackoff:        10 * time.Millisecond,
		MaxBackoff:        40 * time.Millisecond,
		NotRegisteredPoll: 15 * time.Millisecond,
		Refresh:           time.Hour,
		AttemptTimeout:    2 * time.Second,
	})
	return v
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func never(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		if cond() {
			t.Fatalf("unexpected: %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

var managerLink = link.LinkInfo{NodeID: "node_mgr", Transport: types.TransportLocal, RemoteTarget: true}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestEnsureCreatesTheConnectionWhenTheCloudHasNone(t *testing.T) {
	v := newEnsureEnv(t, "node_mgr")
	v.e.NodeLinked(managerLink)

	eventually(t, "the agent to be told", func() bool { return len(v.agents.received("node_mgr")) == 1 })
	if got := v.agents.received("node_mgr")[0]; got != "conn_new1" {
		t.Fatalf("agent got %q", got)
	}
	if _, creates := v.cloud.counts(); creates != 1 {
		t.Fatalf("creates = %d", creates)
	}
	v.cloud.mu.Lock()
	defer v.cloud.mu.Unlock()
	q := v.cloud.queries[0]
	if q.NodeID == nil || *q.NodeID != "node_mgr" || q.Name == nil || *q.Name != "remote-access" {
		t.Fatalf("listed with %+v; the name and node are the contract with the cloud", q)
	}
	if v.cloud.conns[0].Name != "remote-access" || string(v.cloud.conns[0].NodeID) != "node_mgr" {
		t.Fatalf("created %+v", v.cloud.conns[0])
	}
}

func TestEnsureSyncsTheNodesBeforeCreatingTheConnection(t *testing.T) {
	// The cloud knows a cluster's nodes only from the cluster: without the sync
	// the create is a 404 "Node not found" and remote access never comes up.
	v := newEnsureEnv(t, "node_mgr", "node_w1")
	v.e.NodeLinked(managerLink)
	eventually(t, "the agent to be told", func() bool { return len(v.agents.received("node_mgr")) == 1 })
	v.cloud.mu.Lock()
	defer v.cloud.mu.Unlock()
	if len(v.cloud.synced) == 0 || len(v.cloud.synced[0]) != 2 {
		t.Fatalf("synced %v, want the whole registry (both nodes)", v.cloud.synced)
	}
}

func TestEnsureRetriesWhileTheNodeSyncFailsAndNeverCreatesWithoutIt(t *testing.T) {
	v := newEnsureEnv(t, "node_mgr")
	v.cloud.syncErr = errors.New("status 503")
	v.e.NodeLinked(managerLink)
	eventually(t, "a few failed syncs", func() bool { v.cloud.mu.Lock(); defer v.cloud.mu.Unlock(); return v.cloud.syncs >= 3 })
	if lists, creates := v.cloud.counts(); lists+creates != 0 {
		t.Fatalf("touched connections (%d lists, %d creates) although the node sync failed", lists, creates)
	}
	v.cloud.set(func(f *fakeCloud) { f.syncErr = nil })
	eventually(t, "ensure", func() bool { return len(v.agents.received("node_mgr")) >= 1 })
}

func TestEnsureReusesAnExistingNonClosedConnection(t *testing.T) {
	v := newEnsureEnv(t, "node_mgr")
	closedAt := "2026-01-01T00:00:00Z"
	v.cloud.conns = []types.Connection{
		{ID: "conn_closed", NodeID: "node_mgr", Name: "remote-access", State: types.ConnectionStateClosed},
		{ID: "conn_closedat", NodeID: "node_mgr", Name: "remote-access", State: types.ConnectionStateRunning, ClosedAt: &closedAt},
		{ID: "conn_stream", NodeID: "node_mgr", Name: "stream-17", State: types.ConnectionStateRunning},
		{ID: "conn_othernode", NodeID: "node_x", Name: "remote-access", State: types.ConnectionStateRunning},
		{ID: "conn_prov", NodeID: "node_mgr", Name: "remote-access", State: types.ConnectionStateProvisioned, CreatedAt: "2026-02-01T00:00:00Z"},
		{ID: "conn_run_old", NodeID: "node_mgr", Name: "remote-access", State: types.ConnectionStateRunning, CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "conn_run_new", NodeID: "node_mgr", Name: "remote-access", State: types.ConnectionStateRunning, CreatedAt: "2026-03-01T00:00:00Z"},
	}
	v.e.NodeLinked(managerLink)

	eventually(t, "the agent to be told", func() bool { return len(v.agents.received("node_mgr")) == 1 })
	if got := v.agents.received("node_mgr")[0]; got != "conn_run_new" {
		t.Fatalf("agent got %q, want the newest running one", got)
	}
	if _, creates := v.cloud.counts(); creates != 0 {
		t.Fatalf("created %d connections although one could be reused", creates)
	}
}

func TestEnsureDoesNotDuplicateOnReconnects(t *testing.T) {
	v := newEnsureEnv(t, "node_mgr")
	v.e.NodeLinked(managerLink)
	eventually(t, "first ensure", func() bool { return len(v.agents.received("node_mgr")) == 1 })

	// The agent restarts or the link flaps, again and again.
	for i := 2; i <= 4; i++ {
		v.e.NodeLinked(managerLink)
		n := i
		eventually(t, "re-ensure", func() bool { return len(v.agents.received("node_mgr")) == n })
	}
	if _, creates := v.cloud.counts(); creates != 1 {
		t.Fatalf("creates = %d across 4 links, want exactly 1", creates)
	}
	for _, id := range v.agents.received("node_mgr") {
		if id != "conn_new1" {
			t.Fatalf("agent was told %q", id)
		}
	}
}

func TestEnsureDoesNothingWhenTheClusterIsNotRegisteredAndLogsOnce(t *testing.T) {
	v := newEnsureEnv(t, "node_mgr")
	v.setRegistered(false)
	v.e.NodeLinked(managerLink)

	// Several poll rounds pass.
	time.Sleep(120 * time.Millisecond)
	if lists, creates := v.cloud.counts(); lists+creates != 0 {
		t.Fatalf("talked to the cloud while unregistered: %d lists, %d creates", lists, creates)
	}
	if got := v.agents.received("node_mgr"); len(got) != 0 {
		t.Fatalf("agent was told %v", got)
	}
	if n := v.logs.count("not registered"); n != 1 {
		t.Fatalf("logged %d times, want once", n)
	}

	// The cluster registers while running: no restart and no new link needed.
	v.setRegistered(true)
	eventually(t, "ensure after registration", func() bool { return len(v.agents.received("node_mgr")) == 1 })
}

func TestEnsureRetriesWithBackoffWhileTheCloudIsUnreachable(t *testing.T) {
	v := newEnsureEnv(t, "node_mgr")
	v.cloud.listErr = errors.New("dial tcp: connection refused")
	v.e.NodeLinked(managerLink)

	// Retries happen, but not in a hot loop: with a 10ms..40ms backoff, 200ms
	// allows at most ~8 attempts (10+20+40+40+...), far fewer than a spin.
	time.Sleep(200 * time.Millisecond)
	lists, _ := v.cloud.counts()
	if lists < 3 || lists > 12 {
		t.Fatalf("%d attempts in 200ms; expected a handful (backoff)", lists)
	}
	if len(v.agents.received("node_mgr")) != 0 {
		t.Fatal("agent told although the cloud was down")
	}

	// The cloud comes back.
	v.cloud.set(func(f *fakeCloud) { f.listErr = nil })
	eventually(t, "ensure after the cloud returns", func() bool { return len(v.agents.received("node_mgr")) == 1 })
	if _, creates := v.cloud.counts(); creates != 1 {
		t.Fatalf("creates = %d", creates)
	}
}

func TestEnsureRetriesCreateFailureWithoutDuplicates(t *testing.T) {
	v := newEnsureEnv(t, "node_mgr")
	v.cloud.createErr = errors.New("status 503")
	v.e.NodeLinked(managerLink)
	eventually(t, "a few failed creates", func() bool { _, c := v.cloud.counts(); return c >= 3 })
	v.cloud.set(func(f *fakeCloud) { f.createErr = nil })
	eventually(t, "ensure", func() bool { return len(v.agents.received("node_mgr")) >= 1 })
	// Created once more at most by the time it succeeded, and then reused.
	time.Sleep(60 * time.Millisecond)
	v.cloud.mu.Lock()
	n := len(v.cloud.conns)
	v.cloud.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d connections exist, want 1", n)
	}
}

func TestEnsureRetriesWhenTheAgentIsNotReadyYet(t *testing.T) {
	v := newEnsureEnv(t, "node_mgr")
	// The agent has no cloud certificate yet (it was started before registration).
	v.agents.setErr(status.Error(codes.FailedPrecondition, "node has not completed cluster registration"))
	v.e.NodeLinked(managerLink)
	eventually(t, "a few attempts", func() bool { l, _ := v.cloud.counts(); return l >= 3 })
	v.agents.setErr(nil)
	eventually(t, "ensure", func() bool { return len(v.agents.received("node_mgr")) >= 1 })
	if _, creates := v.cloud.counts(); creates != 1 {
		t.Fatalf("creates = %d; retries must reuse the connection", creates)
	}
}

func TestEnsureWaitsForTheLinkInsteadOfPolling(t *testing.T) {
	v := newEnsureEnv(t, "node_mgr")
	v.agents.setLinked("node_mgr", false) // the link dropped right after it came up
	v.e.NodeLinked(managerLink)
	eventually(t, "first attempt", func() bool { l, _ := v.cloud.counts(); return l >= 1 })
	time.Sleep(100 * time.Millisecond)
	if l, _ := v.cloud.counts(); l > 2 {
		t.Fatalf("%d attempts for a node with no link; the next link should trigger it, not a timer", l)
	}
	v.agents.setLinked("node_mgr", true)
	v.e.NodeLinked(managerLink)
	eventually(t, "ensure on the new link", func() bool { return len(v.agents.received("node_mgr")) == 1 })
}

func TestEnsureOnlyForTheManagerLocalAgentWithARemoteTarget(t *testing.T) {
	v := newEnsureEnv(t, "node_mgr", "node_worker", "node_notarget")
	v.e.NodeLinked(link.LinkInfo{NodeID: "node_worker", Transport: types.TransportCluster, RemoteTarget: true}) // joined over mTLS
	v.e.NodeLinked(link.LinkInfo{NodeID: "node_worker", Transport: types.TransportCluster})
	v.e.NodeLinked(link.LinkInfo{NodeID: "node_notarget", Transport: types.TransportLocal}) // no remote target configured
	never(t, "any cloud call for a non-qualifying link", func() bool { l, c := v.cloud.counts(); return l+c > 0 })

	v.e.NodeLinked(managerLink)
	eventually(t, "the manager", func() bool { return len(v.agents.received("node_mgr")) == 1 })
	for _, id := range []string{"node_worker", "node_notarget"} {
		if got := v.agents.received(id); len(got) != 0 {
			t.Fatalf("%s was told %v", id, got)
		}
	}
}

func TestEnsureIsOffWhenTheRemoteListenerIsDisabled(t *testing.T) {
	v := newEnsureEnv(t, "node_mgr")
	v.mu.Lock()
	v.enabled = false
	v.mu.Unlock()
	v.e.NodeLinked(managerLink)
	never(t, "any cloud call with the remote listener off", func() bool { l, c := v.cloud.counts(); return l+c > 0 })
}

func TestEnsureOneOwnerAtATime(t *testing.T) {
	v := newEnsureEnv(t, "node_a", "node_b")
	a := link.LinkInfo{NodeID: "node_a", Transport: types.TransportLocal, RemoteTarget: true}
	b := link.LinkInfo{NodeID: "node_b", Transport: types.TransportLocal, RemoteTarget: true}
	v.e.NodeLinked(a)
	eventually(t, "a", func() bool { return len(v.agents.received("node_a")) == 1 })

	v.e.NodeLinked(b) // a is still linked: it keeps the connection
	never(t, "a second standing connection", func() bool { return len(v.agents.received("node_b")) > 0 })
	if _, creates := v.cloud.counts(); creates != 1 {
		t.Fatalf("creates = %d", creates)
	}

	// a goes away; b may take over.
	v.agents.setLinked("node_a", false)
	v.e.NodeLinked(b)
	eventually(t, "b takes over", func() bool { return len(v.agents.received("node_b")) == 1 })
}

func TestEnsureRefreshHealsAConnectionClosedByTheCloud(t *testing.T) {
	v := newEnsureEnv(t, "node_mgr")
	v.e.cfg.Refresh = 20 * time.Millisecond
	v.e.NodeLinked(managerLink)
	eventually(t, "first ensure", func() bool { return len(v.agents.received("node_mgr")) >= 1 })

	v.cloud.set(func(f *fakeCloud) { f.conns[0].State = types.ConnectionStateClosed })
	eventually(t, "a replacement", func() bool {
		got := v.agents.received("node_mgr")
		return got[len(got)-1] == "conn_new2"
	})
}

func TestPickRemoteConnectionIgnoresEverythingThatIsNotOurs(t *testing.T) {
	if pickRemoteConnection(nil, "n") != nil {
		t.Fatal("nothing to pick")
	}
	got := pickRemoteConnection([]types.Connection{
		{ID: "a", NodeID: "n", Name: "remote-access", State: types.ConnectionStateClosed},
		{ID: "b", NodeID: "n", Name: "remote-access-2", State: types.ConnectionStateRunning},
		{ID: "c", NodeID: "m", Name: "remote-access", State: types.ConnectionStateRunning},
	}, "n")
	if got != nil {
		t.Fatalf("picked %+v", got)
	}
}

func TestCloudConnectionsFromCertsDetectsRegistration(t *testing.T) {
	dir := t.TempDir()
	provider := CloudConnectionsFromCerts(utils.Config{CertDir: dir, ConfigType: utils.OrchestratorConfig})
	if _, err := provider(); !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("no certificate: want ErrNotRegistered, got %v", err)
	}
	// A certificate that exists but cannot be loaded is a real error, not "not
	// registered": it is retried rather than silently ignored.
	if err := os.WriteFile(dir+"/"+types.CertFileNameClusterCert.String(), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := provider(); err == nil || errors.Is(err, ErrNotRegistered) {
		t.Fatalf("broken certificate: want a plain error, got %v", err)
	}
}

func TestEligibleRule(t *testing.T) {
	cases := []struct {
		info link.LinkInfo
		want bool
	}{
		{link.LinkInfo{Transport: types.TransportLocal, RemoteTarget: true}, true},
		{link.LinkInfo{Transport: types.TransportLocal}, false},
		{link.LinkInfo{Transport: types.TransportCluster, RemoteTarget: true}, false},
		{link.LinkInfo{Transport: types.TransportCluster}, false},
	}
	for _, c := range cases {
		if got := Eligible(c.info); got != c.want {
			t.Errorf("%+v: got %v", c.info, got)
		}
	}
}
