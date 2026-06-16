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
