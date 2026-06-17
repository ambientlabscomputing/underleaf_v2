package grpc_private

import (
	"context"
	"fmt"
	"net"
	"os"

	"google.golang.org/grpc"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/service"
)

//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative agent_private.proto

const unixSocketPath = "/tmp/undf-agent.sock"

type AgentGRPCPrivateServer struct {
	UnimplementedAgentPrivateServer
	Service *service.AppService
}

// Serve starts the gRPC private server on a unix socket for CLI access.
func (s *AgentGRPCPrivateServer) Serve() {
	if err := os.Remove(unixSocketPath); err != nil && !os.IsNotExist(err) {
		panic(fmt.Sprintf("grpc_private: failed to remove stale socket: %v", err))
	}

	lis, err := net.Listen("unix", unixSocketPath)
	if err != nil {
		panic(fmt.Sprintf("grpc_private: failed to listen on unix socket: %v", err))
	}

	grpcServer := grpc.NewServer()
	RegisterAgentPrivateServer(grpcServer, s)

	fmt.Printf("gRPC private server listening on unix://%s\n", unixSocketPath)
	if err := grpcServer.Serve(lis); err != nil {
		panic(fmt.Sprintf("grpc_private: server failed: %v", err))
	}
}

// GetStatus implements AgentPrivateServer.
func (s *AgentGRPCPrivateServer) GetStatus(_ context.Context, _ *GetStatusRequest) (*GetStatusResponse, error) {
	return &GetStatusResponse{Status: "ok"}, nil
}

// Ping implements AgentPrivateServer — calls Health.Ping locally and Health.PingPeer toward the orchestrator.
func (s *AgentGRPCPrivateServer) Ping(ctx context.Context, _ *PingRequest) (*PingResponse, error) {
	local, err := s.Service.Health().Ping("cli")
	if err != nil {
		return nil, err
	}

	resp := &PingResponse{
		AgentStatus:      local.Responder,
		AgentTimestampMs: local.TimestampUnixMs,
	}

	peer, peerErr := s.Service.Health().PingPeer(ctx)
	if peerErr != nil {
		resp.OrchestratorReachable = false
		resp.OrchestratorError = peerErr.Error()
	} else {
		resp.OrchestratorReachable = true
		resp.OrchestratorResponder = peer.Responder
		resp.OrchestratorTimestampMs = peer.TimestampUnixMs
	}

	return resp, nil
}

// Register implements AgentPrivateServer.
func (s *AgentGRPCPrivateServer) Register(ctx context.Context, req *RegisterRequest) (*RegisterResponse, error) {
	node, err := s.Service.Orchestrator().RegisterNode(ctx)
	if err != nil {
		return nil, err
	}
	return &RegisterResponse{
		Status: "ok",
		Node: &Node{
			Id:   node.ID,
			Name: node.Name,
		},
	}, nil
}
