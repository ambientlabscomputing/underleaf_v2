package service

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	agentpb "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	_ "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository/migrations"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// fakeAgents is an AgentDirectory whose agents record the commands they get.
type fakeAgents struct {
	mu     sync.Mutex
	linked map[string]bool
	calls  map[string][]string // node id -> commands received
}

func newFakeAgents(linked ...string) *fakeAgents {
	f := &fakeAgents{linked: map[string]bool{}, calls: map[string][]string{}}
	for _, id := range linked {
		f.linked[id] = true
	}
	return f
}

func (f *fakeAgents) setLinked(id string, up bool) {
	f.mu.Lock()
	f.linked[id] = up
	f.mu.Unlock()
}

func (f *fakeAgents) LinkedNodes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	for id, up := range f.linked {
		if up {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func (f *fakeAgents) Agent(id string) clients.AgentAPI { return &fakeAgent{f: f, id: id} }

func (f *fakeAgents) callsTo(id string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls[id]...)
}

type fakeAgent struct {
	f  *fakeAgents
	id string
}

func (a *fakeAgent) rec(c string) error {
	a.f.mu.Lock()
	defer a.f.mu.Unlock()
	if !a.f.linked[a.id] {
		return &clients.NoLinkError{NodeID: a.id}
	}
	a.f.calls[a.id] = append(a.f.calls[a.id], c)
	return nil
}

func (a *fakeAgent) Ping(context.Context, string) (*clients.PingResult, error) {
	return &clients.PingResult{Responder: a.id}, a.rec("ping")
}
func (a *fakeAgent) IngestContainers(context.Context) (int, error) { return 1, a.rec("ingest") }
func (a *fakeAgent) GetContainerLogs(context.Context, string, int64, int64, int, int64) (*agentpb.GetContainerLogsResponse, error) {
	return &agentpb.GetContainerLogsResponse{}, a.rec("logs")
}
func (a *fakeAgent) StreamContainerLogs(context.Context, string, int64) (<-chan *agentpb.LogLine, error) {
	return nil, a.rec("stream-logs")
}
func (a *fakeAgent) GetNode(context.Context) (*agentpb.Node, error) {
	return &agentpb.Node{Id: a.id}, a.rec("get-node")
}
func (a *fakeAgent) NewStream(context.Context, string, string, string, int) error {
	return a.rec("new-stream")
}
func (a *fakeAgent) CloseStream(context.Context, string) error { return a.rec("close-stream") }
func (a *fakeAgent) GetStream(context.Context, string) (*agentpb.StreamState, error) {
	return &agentpb.StreamState{}, a.rec("get-stream")
}
func (a *fakeAgent) ListStreams(context.Context) ([]*agentpb.StreamState, error) {
	return nil, a.rec("list-streams")
}
func (a *fakeAgent) CreateVolume(_ context.Context, name, _ string, _ map[string]string) error {
	return a.rec("create-volume " + name)
}
func (a *fakeAgent) ListVolumes(context.Context) ([]*agentpb.VolumeInfo, error) {
	return nil, a.rec("list-volumes")
}
func (a *fakeAgent) CreateContainer(_ context.Context, r clients.CreateContainerRequest) (string, error) {
	return "d-" + r.Name, a.rec("create-container " + r.Name)
}
func (a *fakeAgent) StartContainer(_ context.Context, n string) error {
	return a.rec("start-container " + n)
}
func (a *fakeAgent) StopContainer(_ context.Context, n string) error {
	return a.rec("stop-container " + n)
}
func (a *fakeAgent) RemoveContainer(_ context.Context, n string) error {
	return a.rec("remove-container " + n)
}

type reconEnv struct {
	repo   *repository.Repository
	agents *fakeAgents
	rec    *ReconcilerService
	dep    *types.Deployment
}

func newReconEnv(t *testing.T, nodeIDs []string, linked ...string) *reconEnv {
	t.Helper()
	repo, err := repository.NewRepositoryAt(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	for _, id := range nodeIDs {
		if err := repo.Nodes.UpsertClusterNode(&types.Node{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	fa := newFakeAgents(linked...)
	dep := &types.Deployment{ID: "deployment_1", Repo: "o/r", Ref: "main", Spec: types.DeploymentSpec{
		Name:    "app",
		Slug:    "app",
		Volumes: []types.VolumeSpec{{Name: "data"}},
		Services: []types.ServiceSpec{{
			Name: "web", Image: "nginx", Volumes: []string{"data:/d"},
		}},
	}}
	if err := repo.Deployments.CreateDeployment(dep); err != nil {
		t.Fatal(err)
	}
	return &reconEnv{repo: repo, agents: fa, rec: &ReconcilerService{Repository: repo, Agents: fa}, dep: dep}
}

func TestReconcilerCommandsTheLinkedNodeNotAnAddress(t *testing.T) {
	e := newReconEnv(t, []string{"node_a", "node_b"}, "node_b")
	if err := e.rec.Reconcile(context.Background(), e.dep); err != nil {
		t.Fatal(err)
	}
	if got := e.agents.callsTo("node_a"); len(got) != 0 {
		t.Errorf("the unlinked node_a was commanded: %v", got)
	}
	got := strings.Join(e.agents.callsTo("node_b"), " | ")
	for _, want := range []string{"list-volumes", "create-volume app_data", "create-container app-web", "start-container app-web"} {
		if !strings.Contains(got, want) {
			t.Errorf("node_b did not receive %q; got: %s", want, got)
		}
	}
	// The placement is recorded.
	if id, _ := e.repo.Deployments.GetNodeID(e.dep.ID); id != "node_b" {
		t.Errorf("placement = %q, want node_b", id)
	}
}

func TestReconcilerFailsClearlyWithNoLinkedNode(t *testing.T) {
	e := newReconEnv(t, []string{"node_a"} /* registered, but nobody linked */)
	err := e.rec.Reconcile(context.Background(), e.dep)
	if err == nil || !strings.Contains(err.Error(), "no node has an active agent link") {
		t.Fatalf("err = %v, want a clear no-link error", err)
	}
	if id, _ := e.repo.Deployments.GetNodeID(e.dep.ID); id != "" {
		t.Errorf("a failed reconcile recorded a placement (%q)", id)
	}
}

func TestReconcilerIgnoresLinksOfUnregisteredNodes(t *testing.T) {
	e := newReconEnv(t, []string{"node_a"}, "node_ghost") // linked but not in the registry
	if err := e.rec.Reconcile(context.Background(), e.dep); err == nil {
		t.Fatal("reconciled onto a node that is not registered")
	}
	if len(e.agents.callsTo("node_ghost")) != 0 {
		t.Error("an unregistered node was commanded")
	}
}

func TestDeploymentStaysOnItsNodeAndFailsWhenThatNodeIsDown(t *testing.T) {
	e := newReconEnv(t, []string{"node_a", "node_b"}, "node_b")
	if err := e.rec.Reconcile(context.Background(), e.dep); err != nil {
		t.Fatal(err)
	}
	// A lower-id node links later: the deployment must not drift to it.
	e.agents.setLinked("node_a", true)
	if err := e.rec.Reconcile(context.Background(), e.dep); err != nil {
		t.Fatal(err)
	}
	if got := e.agents.callsTo("node_a"); len(got) != 0 {
		t.Errorf("deployment drifted to node_a: %v", got)
	}
	// Its own node drops: a clear error, not a silent redeploy elsewhere.
	e.agents.setLinked("node_b", false)
	err := e.rec.Reconcile(context.Background(), e.dep)
	var nl *clients.NoLinkError
	if !errors.As(err, &nl) || nl.NodeID != "node_b" {
		t.Fatalf("err = %v, want *NoLinkError for node_b", err)
	}
	if got := e.agents.callsTo("node_a"); len(got) != 0 {
		t.Errorf("deployment was moved to node_a after node_b went down: %v", got)
	}
}

func TestReconcilerSkipsLowestIDWhenItHasNoLink(t *testing.T) {
	e := newReconEnv(t, []string{"node_a", "node_b", "node_c"}, "node_c", "node_b")
	if err := e.rec.Reconcile(context.Background(), e.dep); err != nil {
		t.Fatal(err)
	}
	if id, _ := e.repo.Deployments.GetNodeID(e.dep.ID); id != "node_b" {
		t.Errorf("placed on %q, want the first linked node by id (node_b)", id)
	}
}

func TestHealthServiceTalksToEveryLinkedNode(t *testing.T) {
	fa := newFakeAgents("node_a", "node_b")
	h := &HealthService{agents: fa}
	n, err := h.IngestAgentContainers(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("ingest = %d, %v; want 2 (one per linked node)", n, err)
	}
	for _, id := range []string{"node_a", "node_b"} {
		if got := fa.callsTo(id); len(got) != 1 || got[0] != "ingest" {
			t.Errorf("%s calls = %v", id, got)
		}
	}
	if r, err := h.PingPeer(context.Background()); err != nil || r.Responder != "node_a" {
		t.Errorf("PingPeer = %+v, %v", r, err)
	}

	none := &HealthService{agents: newFakeAgents()}
	if _, err := none.IngestAgentContainers(context.Background()); !errors.Is(err, ErrNoAgents) {
		t.Errorf("ingest with no links: %v, want ErrNoAgents", err)
	}
	if _, err := none.PingPeer(context.Background()); !errors.Is(err, ErrNoAgents) {
		t.Errorf("ping with no links: %v, want ErrNoAgents", err)
	}
}
