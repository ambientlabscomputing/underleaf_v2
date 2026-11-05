package link

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	agentpb "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// Metadata keys of the LogStream sub-stream.
const (
	// MetaStreamToken carries the capability issued by an OpenLogStream command.
	MetaStreamToken = "x-link-stream-token"
	// MetaNodeID names the node on the local listener (no certificate there).
	MetaNodeID = "x-link-node-id"
)

const helloTimeout = 10 * time.Second

// Authenticator decides which node a link (or sub-stream) belongs to.
//
// The two Orchestrator listeners authenticate differently, and that difference is
// the whole security story of the link:
//
//   - cluster listener: the node id is the CN of the client certificate that
//     passed the mTLS handshake. Nothing the agent says can change it.
//   - local listener: there is no credential. The listener is loopback-only and
//     unauthenticated by design (RFD 5, "Local trust"), so the agent *claims* a
//     node id. See LocalAuth for what that can and cannot do.
type Authenticator interface {
	Authenticate(ctx context.Context, claimedNodeID string) (nodeID, transport string, err error)
}

// NodeLookup is the part of the node repository the local authenticator needs.
type NodeLookup interface {
	GetNodeByID(id string) (*types.Node, error)
}

// LocalAuth authenticates links on the local (loopback, unauthenticated)
// listener.
//
// A local-listener link is *not* proof of anything: any process that can open
// a TCP connection to 127.0.0.1 (or, with the Docker image, to the published
// port) can open one. What the design guarantees instead is the blast radius:
//
//   - It can only claim a node whose transport is "local", i.e. the manager's
//     own agent registered through the local listener. It cannot take over a
//     remote node's link: those nodes are `cluster` nodes and are reachable
//     only through the cluster listener with their certificate.
//   - A link carries commands *from* the Orchestrator *to* the agent. A rogue
//     local link therefore receives the commands meant for the local node
//     (container specs, environment variables) and can answer them falsely or
//     keep the real agent off the link, but it cannot make the Orchestrator or
//     the agent do anything. It gains no capability the local REST API and
//     local gRPC listener (also unauthenticated, loopback-only) do not already
//     give to a local process, and it is the reverse of today's exposure, where
//     a local process could drive the agent's Docker through :50101.
type LocalAuth struct{ Nodes NodeLookup }

func (a LocalAuth) Authenticate(_ context.Context, claimed string) (string, string, error) {
	if claimed == "" {
		return "", "", status.Error(codes.InvalidArgument, "the local listener needs the agent to name its node")
	}
	n, err := a.Nodes.GetNodeByID(claimed)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", status.Errorf(codes.NotFound, "node %s is not registered (run `ufagent register`)", claimed)
		}
		return "", "", status.Error(codes.Internal, "node lookup failed")
	}
	if n.Transport != types.TransportLocal {
		return "", "", status.Errorf(codes.PermissionDenied,
			"node %s joined through the cluster listener; it must connect there with its certificate, not through the local listener", claimed)
	}
	return n.ID, types.TransportLocal, nil
}

// ClusterAuth authenticates links on the cluster listener from the verified
// client certificate. Identity returns the authenticated node id for ctx (the
// cluster listener's interceptors put it there after re-checking revocation
// and membership).
type ClusterAuth struct {
	Identity func(ctx context.Context) (string, bool)
}

func (a ClusterAuth) Authenticate(ctx context.Context, claimed string) (string, string, error) {
	id, ok := a.Identity(ctx)
	if !ok {
		return "", "", status.Error(codes.Unauthenticated, "no authenticated node identity")
	}
	if claimed != "" && claimed != id {
		return "", "", status.Errorf(codes.PermissionDenied, "this link authenticates as %s, not %s", id, claimed)
	}
	return id, types.TransportCluster, nil
}

// Server implements agentpb.AgentLinkServer for one listener.
type Server struct {
	agentpb.UnimplementedAgentLinkServer
	Hub  *Hub
	Auth Authenticator
}

// NewServer returns the link service for a listener with the given Authenticator.
func NewServer(h *Hub, a Authenticator) *Server { return &Server{Hub: h, Auth: a} }

// Link implements AgentLinkServer.
func (s *Server) Link(stream agentpb.AgentLink_LinkServer) error {
	ctx := stream.Context()

	type recvd struct {
		msg *agentpb.AgentMessage
		err error
	}
	in := make(chan recvd)
	go func() {
		defer close(in)
		for {
			m, err := stream.Recv()
			select {
			case in <- recvd{m, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	// The first message must be a hello, promptly.
	var hello *agentpb.LinkHello
	select {
	case r := <-in:
		if r.err != nil {
			return r.err
		}
		hello = r.msg.GetHello()
	case <-time.After(helloTimeout):
		return status.Error(codes.DeadlineExceeded, "no hello received")
	case <-ctx.Done():
		return ctx.Err()
	}
	if hello == nil {
		return status.Error(codes.InvalidArgument, "the first message on a link must be a hello")
	}
	nodeID, transport, err := s.Auth.Authenticate(ctx, hello.GetNodeId())
	if err != nil {
		return err
	}

	sess := newSession(nodeID, transport, stream.Send)
	s.Hub.register(sess)
	defer s.Hub.unregister(sess)
	if err := sess.sendMsg(&agentpb.OrchestratorMessage{Msg: &agentpb.OrchestratorMessage_Welcome{
		Welcome: &agentpb.LinkWelcome{NodeId: nodeID, Transport: transport},
	}}); err != nil {
		return err
	}
	fmt.Printf("[link] node %s linked (%s listener)\n", nodeID, transport)
	defer fmt.Printf("[link] node %s link closed\n", nodeID)

	for {
		select {
		case r, ok := <-in:
			if !ok || r.err != nil {
				if r.err == nil || errors.Is(r.err, io.EOF) {
					return nil
				}
				return r.err
			}
			if res := r.msg.GetResult(); res != nil {
				// Answers that match nothing (late, duplicate, or invented) are
				// ignored: a request id is only honoured for the link it was
				// issued on, once.
				sess.deliver(res)
			}
			// Further hellos or unknown messages are ignored.
		case <-sess.closed:
			return status.Error(codes.Unavailable, sess.reason.Error())
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// LogStream implements AgentLinkServer: the sub-stream of one OpenLogStream command.
func (s *Server) LogStream(stream agentpb.AgentLink_LogStreamServer) error {
	ctx := stream.Context()
	md, _ := metadata.FromIncomingContext(ctx)
	var claimed, token string
	if v := md.Get(MetaNodeID); len(v) > 0 {
		claimed = v[0]
	}
	if v := md.Get(MetaStreamToken); len(v) > 0 {
		token = v[0]
	}
	nodeID, _, err := s.Auth.Authenticate(ctx, claimed)
	if err != nil {
		return err
	}
	ls, err := s.Hub.attachStream(token, nodeID)
	if err != nil {
		return err
	}
	defer ls.closeLines()
	for {
		line, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		select {
		case ls.lines <- line:
		case <-ls.done:
			return status.Error(codes.Canceled, "log stream closed by the Orchestrator")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
