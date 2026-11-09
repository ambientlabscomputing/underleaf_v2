package link_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	agentpb "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/link"
	olink "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/link"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// ── harness ───────────────────────────────────────────────────────────────────

type fakeNodes map[string]*types.Node

func (f fakeNodes) GetNodeByID(id string) (*types.Node, error) {
	if n, ok := f[id]; ok {
		return n, nil
	}
	return nil, sql.ErrNoRows
}

type handlerFunc func(ctx context.Context, cmd *agentpb.Command, env link.Env) *agentpb.CommandResult

func (f handlerFunc) Handle(ctx context.Context, cmd *agentpb.Command, env link.Env) *agentpb.CommandResult {
	return f(ctx, cmd, env)
}

type harness struct {
	t     *testing.T
	hub   *olink.Hub
	nodes fakeNodes
	addr  string
	mu    sync.Mutex
	gs    *grpc.Server
}

func newHarness(t *testing.T) *harness {
	h := &harness{t: t, hub: olink.NewHub(), nodes: fakeNodes{
		"node_local":  {ID: "node_local", Transport: types.TransportLocal},
		"node_other":  {ID: "node_other", Transport: types.TransportLocal},
		"node_remote": {ID: "node_remote", Transport: types.TransportCluster},
	}}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h.addr = lis.Addr().String()
	h.serve(lis)
	t.Cleanup(func() { h.stop() })
	return h
}

func (h *harness) serve(lis net.Listener) {
	gs := grpc.NewServer()
	agentpb.RegisterAgentLinkServer(gs, olink.NewServer(h.hub, olink.LocalAuth{Nodes: h.nodes}))
	go gs.Serve(lis)
	h.mu.Lock()
	h.gs = gs
	h.mu.Unlock()
}

func (h *harness) stop() {
	h.mu.Lock()
	gs := h.gs
	h.mu.Unlock()
	if gs != nil {
		gs.Stop()
	}
}

// restart kills the server (and every connection to it) and brings it back on
// the same address.
func (h *harness) restart() {
	h.stop()
	var lis net.Listener
	var err error
	for i := 0; i < 50; i++ {
		if lis, err = net.Listen("tcp", h.addr); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		h.t.Fatalf("re-listen: %v", err)
	}
	h.serve(lis)
}

func (h *harness) agent(nodeID string, handler link.Handler) (cancel func(), states chan bool) {
	states = make(chan bool, 64)
	c := &link.Client{
		Dial: func(ctx context.Context) (*link.Target, error) {
			conn, err := grpc.NewClient(h.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				return nil, err
			}
			return &link.Target{Conn: conn, NodeID: nodeID, Local: true}, nil
		},
		Handler:    handler,
		MinBackoff: 20 * time.Millisecond, MaxBackoff: 100 * time.Millisecond, StableAfter: time.Second,
		OnState: func(up bool) {
			select {
			case states <- up:
			default:
			}
		},
	}
	ctx, cancelCtx := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	return func() { cancelCtx(); <-done }, states
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// echo answers Ping with the request's source as the responder, so a caller can
// tell whether it got *its own* answer.
var echo = handlerFunc(func(ctx context.Context, cmd *agentpb.Command, env link.Env) *agentpb.CommandResult {
	switch c := cmd.GetCmd().(type) {
	case *agentpb.Command_Ping:
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_Ping{Ping: &agentpb.PingResponse{Responder: c.Ping.GetSource(), TimestampUnixMs: 1}}}
	case *agentpb.Command_GetNode:
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_Node{Node: &agentpb.Node{Id: env.NodeID, Name: "fake"}}}
	case *agentpb.Command_ListVolumes:
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_Volumes{Volumes: &agentpb.ListVolumesResponse{Volumes: []*agentpb.VolumeInfo{{Name: "v1"}}}}}
	case *agentpb.Command_CreateContainer:
		r := c.CreateContainer
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_CreateContainer{CreateContainer: &agentpb.CreateContainerResponse{DockerId: "docker-" + r.Name + "-" + r.Image + "-" + r.Build.GetContext()}}}
	case *agentpb.Command_StartContainer:
		if c.StartContainer.Name == "boom" {
			return &agentpb.CommandResult{Error: &agentpb.CommandError{Code: int32(codes.NotFound), Message: "no such container: boom"}}
		}
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_Ok{Ok: &emptypb.Empty{}}}
	}
	return &agentpb.CommandResult{Error: &agentpb.CommandError{Code: int32(codes.Unimplemented), Message: "unhandled"}}
})

// ── tests ─────────────────────────────────────────────────────────────────────

func TestCommandsRoundTripOverTheLink(t *testing.T) {
	h := newHarness(t)
	stop, _ := h.agent("node_local", echo)
	defer stop()
	waitFor(t, "link up", func() bool { return h.hub.Linked("node_local") })

	ag := h.hub.Agent("node_local")
	ctx := context.Background()

	p, err := ag.Ping(ctx, "orchestrator")
	if err != nil || p.Responder != "orchestrator" {
		t.Fatalf("ping = %+v, %v", p, err)
	}
	n, err := ag.GetNode(ctx)
	if err != nil || n.Id != "node_local" {
		t.Fatalf("get node = %+v, %v", n, err)
	}
	vols, err := ag.ListVolumes(ctx)
	if err != nil || len(vols) != 1 || vols[0].Name != "v1" {
		t.Fatalf("volumes = %+v, %v", vols, err)
	}
	id, err := ag.CreateContainer(ctx, clients.CreateContainerRequest{Name: "c", Image: "img", Build: &clients.BuildSourceRequest{Context: "ctx"}})
	if err != nil || id != "docker-c-img-ctx" {
		t.Fatalf("create container = %q, %v (request fields must survive the trip)", id, err)
	}
	if err := ag.StartContainer(ctx, "c"); err != nil {
		t.Fatal(err)
	}
	// An error from the agent keeps its gRPC code.
	err = ag.StartContainer(ctx, "boom")
	if status.Code(err) != codes.NotFound {
		t.Fatalf("agent error code = %v (%v), want NotFound", status.Code(err), err)
	}
}

func TestNoLinkIsAClearError(t *testing.T) {
	h := newHarness(t)
	_, err := h.hub.Agent("node_local").Ping(context.Background(), "x")
	var nl *clients.NoLinkError
	if !errors.As(err, &nl) || nl.NodeID != "node_local" {
		t.Fatalf("err = %v, want *NoLinkError for node_local", err)
	}
	if len(h.hub.LinkedNodes()) != 0 {
		t.Error("LinkedNodes not empty")
	}

	// And after the agent goes away again.
	stop, _ := h.agent("node_local", echo)
	waitFor(t, "link up", func() bool { return h.hub.Linked("node_local") })
	if _, err := h.hub.Agent("node_local").Ping(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	stop()
	waitFor(t, "link gone", func() bool { return !h.hub.Linked("node_local") })
	if _, err := h.hub.Agent("node_local").Ping(context.Background(), "x"); !errors.As(err, &nl) {
		t.Fatalf("after the agent left: err = %v, want *NoLinkError", err)
	}
}

func TestRequestIDsMatchUnderConcurrency(t *testing.T) {
	h := newHarness(t)
	// A handler that answers in a scrambled order: later requests often finish
	// first, so only correct request-id matching returns the right answers.
	var seq atomic.Int64
	scrambled := handlerFunc(func(ctx context.Context, cmd *agentpb.Command, env link.Env) *agentpb.CommandResult {
		time.Sleep(time.Duration((seq.Add(1)*7)%23) * time.Millisecond)
		return echo(ctx, cmd, env)
	})
	stop, _ := h.agent("node_local", scrambled)
	defer stop()
	waitFor(t, "link up", func() bool { return h.hub.Linked("node_local") })

	const n = 300
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			want := fmt.Sprintf("caller-%d", i)
			p, err := h.hub.Agent("node_local").Ping(context.Background(), want)
			switch {
			case err != nil:
				errs <- err
			case p.Responder != want:
				errs <- fmt.Errorf("caller %d got the answer for %q", i, p.Responder)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestLinkReconnectsAfterServerSideDrop(t *testing.T) {
	h := newHarness(t)
	stop, states := h.agent("node_local", echo)
	defer stop()
	waitFor(t, "link up", func() bool { return h.hub.Linked("node_local") })
	<-states // true

	h.hub.Drop("node_local") // the Orchestrator closes the link (e.g. nodes rm)
	waitFor(t, "agent to notice and re-link", func() bool {
		if !h.hub.Linked("node_local") {
			return false
		}
		_, err := h.hub.Agent("node_local").Ping(context.Background(), "again")
		return err == nil
	})
}

func TestLinkReconnectsAfterTheConnectionIsKilled(t *testing.T) {
	h := newHarness(t)
	stop, _ := h.agent("node_local", echo)
	defer stop()
	waitFor(t, "link up", func() bool { return h.hub.Linked("node_local") })

	h.restart() // every TCP connection dies; the server comes back on the same port
	waitFor(t, "agent to re-link after the outage", func() bool {
		if !h.hub.Linked("node_local") {
			return false
		}
		p, err := h.hub.Agent("node_local").Ping(context.Background(), "back")
		return err == nil && p.Responder == "back"
	})
}

func TestInFlightCommandIsReleasedWhenTheLinkDrops(t *testing.T) {
	h := newHarness(t)
	started := make(chan struct{})
	hang := handlerFunc(func(ctx context.Context, cmd *agentpb.Command, env link.Env) *agentpb.CommandResult {
		close(started)
		<-ctx.Done() // never answers
		return nil
	})
	stop, _ := h.agent("node_local", hang)
	defer stop()
	waitFor(t, "link up", func() bool { return h.hub.Linked("node_local") })

	errc := make(chan error, 1)
	go func() {
		_, err := h.hub.Agent("node_local").Ping(context.Background(), "x")
		errc <- err
	}()
	<-started
	h.hub.Drop("node_local")
	select {
	case err := <-errc:
		if !errors.Is(err, olink.ErrLinkDropped) {
			t.Fatalf("err = %v, want ErrLinkDropped", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a caller was left hanging after the link dropped")
	}
}

func TestCancellationReachesTheAgent(t *testing.T) {
	h := newHarness(t)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	blocked := handlerFunc(func(ctx context.Context, cmd *agentpb.Command, env link.Env) *agentpb.CommandResult {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil
	})
	stop, _ := h.agent("node_local", blocked)
	defer stop()
	waitFor(t, "link up", func() bool { return h.hub.Linked("node_local") })

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { _, err := h.hub.Agent("node_local").Ping(ctx, "x"); errc <- err }()
	<-started
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("caller err = %v, want context.Canceled", err)
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("the agent's handler was never told the caller gave up")
	}
}

func TestANewerLinkSupersedesAGhost(t *testing.T) {
	h := newHarness(t)
	stop1, _ := h.agent("node_local", echo)
	defer stop1()
	waitFor(t, "first link", func() bool { return h.hub.Linked("node_local") })
	stop2, _ := h.agent("node_local", echo) // e.g. the old connection is half-dead
	defer stop2()
	time.Sleep(200 * time.Millisecond)
	if _, err := h.hub.Agent("node_local").Ping(context.Background(), "x"); err != nil {
		t.Fatalf("after a second link for the same node: %v", err)
	}
	if got := h.hub.LinkedNodes(); len(got) != 1 {
		t.Fatalf("linked nodes = %v, want exactly one session per node", got)
	}
}

func TestLocalListenerRefusesRemoteAndUnknownNodes(t *testing.T) {
	h := newHarness(t)
	for _, id := range []string{"node_remote", "node_nobody", ""} {
		var lastErr atomic.Value
		c := &link.Client{
			Dial: func(ctx context.Context) (*link.Target, error) {
				conn, err := grpc.NewClient(h.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
				return &link.Target{Conn: conn, NodeID: id, Local: true}, err
			},
			Handler:    echo,
			MinBackoff: 10 * time.Millisecond, MaxBackoff: 20 * time.Millisecond,
			Logf: func(format string, args ...any) { lastErr.Store(fmt.Sprintf(format, args...)) },
		}
		ctx, cancel := context.WithCancel(context.Background())
		go c.Run(ctx)
		time.Sleep(300 * time.Millisecond)
		cancel()
		if h.hub.Linked(id) {
			t.Errorf("node %q was linked through the local listener", id)
		}
		msg, _ := lastErr.Load().(string)
		if msg == "" {
			t.Errorf("node %q: the agent was not told why it was refused", id)
		}
	}
	if len(h.hub.LinkedNodes()) != 0 {
		t.Fatalf("linked = %v", h.hub.LinkedNodes())
	}
}

func TestClusterAuthTakesIdentityFromTheCertificateOnly(t *testing.T) {
	auth := olink.ClusterAuth{Identity: func(ctx context.Context) (string, bool) {
		id, ok := ctx.Value(testKey{}).(string)
		return id, ok
	}}
	ctx := context.WithValue(context.Background(), testKey{}, "node_cert")
	id, transport, err := auth.Authenticate(ctx, "")
	if err != nil || id != "node_cert" || transport != types.TransportCluster {
		t.Fatalf("empty claim: %q %q %v", id, transport, err)
	}
	if _, _, err := auth.Authenticate(ctx, "node_cert"); err != nil {
		t.Fatalf("matching claim: %v", err)
	}
	if _, _, err := auth.Authenticate(ctx, "node_victim"); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a member claiming another node's id: err = %v, want PermissionDenied", err)
	}
	if _, _, err := auth.Authenticate(context.Background(), ""); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("no certificate: err = %v, want Unauthenticated", err)
	}
}

type testKey struct{}

func TestReconnectBackoffGrowsAndIsCapped(t *testing.T) {
	var mu sync.Mutex
	var at []time.Time
	c := &link.Client{
		Dial: func(ctx context.Context) (*link.Target, error) {
			mu.Lock()
			at = append(at, time.Now())
			mu.Unlock()
			return nil, errors.New("manager down")
		},
		Handler:    echo,
		MinBackoff: 20 * time.Millisecond, MaxBackoff: 100 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
	defer cancel()
	c.Run(ctx)

	mu.Lock()
	defer mu.Unlock()
	if len(at) < 6 {
		t.Fatalf("only %d attempts; the client must keep retrying", len(at))
	}
	gap := func(i int) time.Duration { return at[i+1].Sub(at[i]) }
	for i := 0; i < 2; i++ { // backoffs 20 -> 40 -> 80ms: with +-20% jitter each step is at least 1.33x the last
		if gap(i+1) <= gap(i) {
			t.Errorf("gap %d (%v) did not grow past gap %d (%v)", i+1, gap(i+1), i, gap(i))
		}
	}
	for i := 2; i < len(at)-1; i++ { // from 80ms on the cap (100ms) applies
		if gap(i) > 160*time.Millisecond {
			t.Errorf("gap %d = %v exceeds the cap (100ms +20%% jitter + scheduling)", i, gap(i))
		}
	}
}

func TestLogSubStream(t *testing.T) {
	h := newHarness(t)
	var tokenSeen atomic.Value
	streaming := handlerFunc(func(ctx context.Context, cmd *agentpb.Command, env link.Env) *agentpb.CommandResult {
		o, ok := cmd.GetCmd().(*agentpb.Command_OpenLogStream)
		if !ok {
			return echo(ctx, cmd, env)
		}
		tokenSeen.Store(o.OpenLogStream.StreamToken)
		go func() {
			md := metadata.Pairs("x-link-stream-token", o.OpenLogStream.StreamToken, "x-link-node-id", env.NodeID)
			sctx, cancel := context.WithCancel(metadata.NewOutgoingContext(env.LinkCtx, md))
			defer cancel()
			st, err := agentpb.NewAgentLinkClient(env.Conn).LogStream(sctx)
			if err != nil {
				return
			}
			go func() { st.Recv(); cancel() }() // ends when the Orchestrator ends the call
			for i := 0; ; i++ {
				if err := st.Send(&agentpb.LogLine{DockerId: o.OpenLogStream.DockerId, Message: fmt.Sprintf("line %d", i)}); err != nil {
					return
				}
				select {
				case <-sctx.Done():
					return
				case <-time.After(5 * time.Millisecond):
				}
			}
		}()
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_Ok{Ok: &emptypb.Empty{}}}
	})
	stop, _ := h.agent("node_local", streaming)
	defer stop()
	waitFor(t, "link up", func() bool { return h.hub.Linked("node_local") })

	ctx, cancel := context.WithCancel(context.Background())
	lines, err := h.hub.Agent("node_local").StreamContainerLogs(ctx, "dockerX", 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		select {
		case l, ok := <-lines:
			if !ok || l.DockerId != "dockerX" || l.Message != fmt.Sprintf("line %d", i) {
				t.Fatalf("line %d = %+v ok=%v", i, l, ok)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("no log line")
		}
	}
	cancel() // the consumer goes away: the sub-stream must end and the channel close
	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-lines:
			if !ok {
				goto closed
			}
		case <-deadline:
			t.Fatal("log channel not closed after cancel")
		}
	}
closed:
	// The token was single-use: replaying it as the same node, or using it as
	// another node, is refused.
	tok, _ := tokenSeen.Load().(string)
	for _, node := range []string{"node_local", "node_other"} {
		conn, _ := grpc.NewClient(h.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		md := metadata.Pairs("x-link-stream-token", tok, "x-link-node-id", node)
		st, err := agentpb.NewAgentLinkClient(conn).LogStream(metadata.NewOutgoingContext(context.Background(), md))
		if err == nil {
			_ = st.Send(&agentpb.LogLine{Message: "forged"})
			_, err = st.Recv()
		}
		if status.Code(err) != codes.PermissionDenied {
			t.Errorf("replaying a spent token as %s: err = %v, want PermissionDenied", node, err)
		}
		conn.Close()
	}
}

func TestLogStreamTokenIsBoundToTheNodeItWasIssuedFor(t *testing.T) {
	h := newHarness(t)
	// node_local is asked to stream; node_other tries to attach to that stream.
	var tok atomic.Value
	open := handlerFunc(func(ctx context.Context, cmd *agentpb.Command, env link.Env) *agentpb.CommandResult {
		if o, ok := cmd.GetCmd().(*agentpb.Command_OpenLogStream); ok {
			tok.Store(o.OpenLogStream.StreamToken)
			return &agentpb.CommandResult{Result: &agentpb.CommandResult_Ok{Ok: &emptypb.Empty{}}}
		}
		return echo(ctx, cmd, env)
	})
	stop, _ := h.agent("node_local", open)
	defer stop()
	waitFor(t, "link up", func() bool { return h.hub.Linked("node_local") })

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		_, err := h.hub.Agent("node_local").StreamContainerLogs(ctx, "d", 0)
		errc <- err
	}()
	waitFor(t, "token issued", func() bool { return tok.Load() != nil })

	conn, _ := grpc.NewClient(h.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	defer conn.Close()
	md := metadata.Pairs("x-link-stream-token", tok.Load().(string), "x-link-node-id", "node_other")
	st, err := agentpb.NewAgentLinkClient(conn).LogStream(metadata.NewOutgoingContext(context.Background(), md))
	if err == nil {
		_ = st.Send(&agentpb.LogLine{Message: "hijack"})
		_, err = st.Recv()
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("another node attached to a stream issued for node_local: err = %v", err)
	}
	<-errc
}
