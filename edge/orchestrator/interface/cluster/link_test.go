package cluster

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	agentpb "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/link"
	olink "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/link"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/pki"
)

// The agent link over the real cluster listener: mTLS identity, revocation.

type echoHandler struct{}

func (echoHandler) Handle(ctx context.Context, cmd *agentpb.Command, env link.Env) *agentpb.CommandResult {
	switch c := cmd.GetCmd().(type) {
	case *agentpb.Command_Ping:
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_Ping{Ping: &agentpb.PingResponse{Responder: env.NodeID + ":" + c.Ping.GetSource()}}}
	case *agentpb.Command_StopContainer:
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_Ok{Ok: &emptypb.Empty{}}}
	}
	return &agentpb.CommandResult{Error: &agentpb.CommandError{Code: 12, Message: "unhandled"}}
}

func (e *env) withLinks(t *testing.T) *olink.Hub {
	t.Helper()
	hub := olink.NewHub()
	srv := olink.NewServer(hub, olink.ClusterAuth{Identity: NodeID})
	e.srv.Register = func(gs *grpc.Server) { agentpb.RegisterAgentLinkServer(gs, srv) }
	e.be.cs.OnNodeRemoved = hub.Drop
	e.be.cs.OnCertsRevoked = hub.Drop
	e.be.cs.Linked = hub.Linked
	return hub
}

// runAgent starts a link client for member m; claim, if non-empty, makes the
// hello name that node id (a lying agent).
func runAgent(t *testing.T, m *pki.Member, claim string) (stop func(), lastErr func() string) {
	t.Helper()
	var msg atomicString
	c := &link.Client{
		Dial: func(ctx context.Context) (*link.Target, error) {
			conn, err := clients.DialCluster(m)
			if err != nil {
				return nil, err
			}
			t := &link.Target{Conn: conn, NodeID: m.State.NodeID}
			if claim != "" {
				t.Local, t.NodeID = true, claim // Local makes the hello carry NodeID
			}
			return t, nil
		},
		Handler:    echoHandler{},
		MinBackoff: 20 * time.Millisecond, MaxBackoff: 80 * time.Millisecond,
		Logf: func(f string, a ...any) { msg.set(sprintf(f, a...)) },
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	return func() { cancel(); <-done }, msg.get
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAgentLinkOverMTLSTakesIdentityFromTheCertificate(t *testing.T) {
	e := newEnv(t)
	hub := e.withLinks(t)
	res, _ := e.be.cs.AddNode(e.addr)
	m := e.joinNode(t, res.JoinString, worker1)

	stop, _ := runAgent(t, m, "")
	defer stop()
	waitUntil(t, "link up", func() bool { return hub.Linked(worker1) })

	p, err := hub.Agent(worker1).Ping(context.Background(), "hi")
	if err != nil || p.Responder != worker1+":hi" {
		t.Fatalf("ping over the cluster link = %+v, %v", p, err)
	}
	if !e.be.cs.IsLinked(worker1) {
		t.Error("`nodes ls` would show the node as unlinked")
	}
}

func TestAgentCannotClaimAnotherNodesLinkOverMTLS(t *testing.T) {
	e := newEnv(t)
	hub := e.withLinks(t)
	res1, _ := e.be.cs.AddNode(e.addr)
	m1 := e.joinNode(t, res1.JoinString, worker1)
	res2, _ := e.be.cs.AddNode(e.addr)
	_ = e.joinNode(t, res2.JoinString, worker2)

	// worker1's certificate, but the hello claims to be worker2.
	stop, lastErr := runAgent(t, m1, worker2)
	defer stop()
	waitUntil(t, "a refusal to be reported", func() bool { return lastErr() != "" })
	time.Sleep(200 * time.Millisecond)
	if hub.Linked(worker2) || hub.Linked(worker1) {
		t.Fatalf("a link was accepted for a lying hello (linked: %v)", hub.LinkedNodes())
	}
}

func TestNodesRmDropsTheLiveLinkAndItCannotComeBack(t *testing.T) {
	e := newEnv(t)
	hub := e.withLinks(t)
	res, _ := e.be.cs.AddNode(e.addr)
	m := e.joinNode(t, res.JoinString, worker1)
	stop, _ := runAgent(t, m, "")
	defer stop()
	waitUntil(t, "link up", func() bool { return hub.Linked(worker1) })

	if _, err := e.be.cs.RemoveNode(worker1); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the live link to be dropped", func() bool { return !hub.Linked(worker1) })
	// The agent keeps retrying with its revoked certificate and must stay out.
	time.Sleep(400 * time.Millisecond)
	if hub.Linked(worker1) {
		t.Fatal("a removed node re-established its link")
	}
	if _, err := hub.Agent(worker1).Ping(context.Background(), "x"); err == nil {
		t.Fatal("a removed node still answers commands")
	}
}

func TestReEnrollmentDropsTheLinkOfTheOldCertificate(t *testing.T) {
	e := newEnv(t)
	hub := e.withLinks(t)
	res, _ := e.be.cs.AddNode(e.addr)
	old := e.joinNode(t, res.JoinString, worker1)
	stopOld, _ := runAgent(t, old, "")
	defer stopOld()
	waitUntil(t, "link up", func() bool { return hub.Linked(worker1) })

	res2, _ := e.be.cs.AddNode(e.addr)
	fresh := e.joinNode(t, res2.JoinString, worker1) // same node id, new certificate
	waitUntil(t, "the old certificate's link to be dropped", func() bool { return !hub.Linked(worker1) })
	stopNew, _ := runAgent(t, fresh, "")
	defer stopNew()
	waitUntil(t, "the re-enrolled node to link", func() bool { return hub.Linked(worker1) })
	if _, err := hub.Agent(worker1).Ping(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
}
