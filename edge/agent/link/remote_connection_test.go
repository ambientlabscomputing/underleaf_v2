package link_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	agentpb "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/link"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/service"
	olink "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/link"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

type recordingStanding struct {
	mu      sync.Mutex
	current string
	history []string
}

func (r *recordingStanding) Start() {}
func (r *recordingStanding) SetConnection(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current == id {
		return false
	}
	r.current = id
	r.history = append(r.history, id)
	return true
}
func (r *recordingStanding) ids() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.history...)
}

func dispatcherWith(rs *recordingStanding, target string) *link.Dispatcher {
	conns := (&service.ConnectionService{}).WithRemote(rs, target, "")
	return &link.Dispatcher{Service: service.NewAppServiceWith(conns)}
}

func setRemoteCmd(id string) *agentpb.Command {
	return &agentpb.Command{RequestId: "req_1", Cmd: &agentpb.Command_SetRemoteConnection{SetRemoteConnection: &agentpb.SetRemoteConnection{ConnectionId: id}}}
}

func TestDispatchSetRemoteConnection(t *testing.T) {
	rs := &recordingStanding{}
	d := dispatcherWith(rs, "127.0.0.1:50110")

	res := d.Handle(context.Background(), setRemoteCmd("conn_a"), link.Env{})
	if res.GetError() != nil || res.GetOk() == nil || res.GetRequestId() != "req_1" {
		t.Fatalf("result = %+v", res)
	}
	// Idempotent for the same id, switches for a new one.
	d.Handle(context.Background(), setRemoteCmd("conn_a"), link.Env{})
	d.Handle(context.Background(), setRemoteCmd("conn_b"), link.Env{})
	if got := rs.ids(); len(got) != 2 || got[0] != "conn_a" || got[1] != "conn_b" {
		t.Fatalf("standing connection history = %v", got)
	}

	// Refusals keep their codes.
	if code := d.Handle(context.Background(), setRemoteCmd(""), link.Env{}).GetError().GetCode(); code != int32(codes.InvalidArgument) {
		t.Fatalf("empty id: code %d", code)
	}
	noTarget := dispatcherWith(&recordingStanding{}, "")
	if code := noTarget.Handle(context.Background(), setRemoteCmd("conn_a"), link.Env{}).GetError().GetCode(); code != int32(codes.FailedPrecondition) {
		t.Fatalf("no target: code %d", code)
	}
	// A node that has not registered with the cloud has no connection service.
	unregistered := &link.Dispatcher{Service: service.NewAppServiceWith(nil)}
	if code := unregistered.Handle(context.Background(), setRemoteCmd("conn_a"), link.Env{}).GetError().GetCode(); code != int32(codes.FailedPrecondition) {
		t.Fatalf("no connection service: code %d", code)
	}
}

// helloAgent runs an agent link like harness.agent, with the hello's
// remote-target flag under the test's control.
func (h *harness) helloAgent(nodeID string, handler link.Handler, remoteTarget bool) (stop func()) {
	c := &link.Client{
		Dial: func(ctx context.Context) (*link.Target, error) {
			conn, err := grpc.NewClient(h.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				return nil, err
			}
			return &link.Target{Conn: conn, NodeID: nodeID, Local: true}, nil
		},
		Handler:      handler,
		RemoteTarget: func() bool { return remoteTarget },
		MinBackoff:   20 * time.Millisecond, MaxBackoff: 100 * time.Millisecond, StableAfter: time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	return func() { cancel(); <-done }
}

// End to end: the Orchestrator's call on the node client reaches the agent's
// dispatcher as the new command, and the hello's remote-target flag shows up in
// the hub (it is what makes an agent a candidate for the standing connection).
func TestSetRemoteConnectionRoundTripAndHelloFlag(t *testing.T) {
	h := newHarness(t)
	linked := make(chan olink.LinkInfo, 8)
	h.hub.OnLinked(func(i olink.LinkInfo) { linked <- i })

	rs := &recordingStanding{}
	stop := h.helloAgent("node_local", dispatcherWith(rs, "127.0.0.1:50110"), true)
	defer stop()
	stopWorkerLike := h.helloAgent("node_other", dispatcherWith(&recordingStanding{}, ""), false)
	defer stopWorkerLike()

	got := map[string]olink.LinkInfo{}
	for len(got) < 2 {
		select {
		case i := <-linked:
			got[i.NodeID] = i
		case <-time.After(5 * time.Second):
			t.Fatalf("OnLinked fired for %v only", got)
		}
	}
	if i := got["node_local"]; i.Transport != types.TransportLocal || !i.RemoteTarget {
		t.Fatalf("node_local info = %+v", i)
	}
	if i := got["node_other"]; i.RemoteTarget {
		t.Fatalf("an agent without a remote target reported one: %+v", i)
	}
	byNode := map[string]olink.LinkInfo{}
	for _, i := range h.hub.Links() {
		byNode[i.NodeID] = i
	}
	if len(byNode) != 2 || !byNode["node_local"].RemoteTarget || byNode["node_other"].RemoteTarget {
		t.Fatalf("Links() = %+v", byNode)
	}

	ctx := context.Background()
	if err := h.hub.Agent("node_local").SetRemoteConnection(ctx, "conn_x"); err != nil {
		t.Fatal(err)
	}
	if err := h.hub.Agent("node_local").SetRemoteConnection(ctx, "conn_x"); err != nil {
		t.Fatal(err)
	}
	if ids := rs.ids(); len(ids) != 1 || ids[0] != "conn_x" {
		t.Fatalf("agent standing connections = %v", ids)
	}
	// The agent without a target refuses, with a code the Orchestrator can act on.
	err := h.hub.Agent("node_other").SetRemoteConnection(ctx, "conn_x")
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("agent without target: %v", err)
	}
}
