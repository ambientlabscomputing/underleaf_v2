package grpc_public

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/service"
)

//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative agent_public.proto

const tcpAddr = ":50101"

type AgentGRPCPublicServer struct {
	UnimplementedAgentPublicServer
	Service *service.AppService
}

// Serve starts the gRPC public server on TCP for orchestrator/agent communication.
func (s *AgentGRPCPublicServer) Serve() {
	lis, err := net.Listen("tcp", tcpAddr)
	if err != nil {
		panic(fmt.Sprintf("grpc_public: failed to listen on %s: %v", tcpAddr, err))
	}

	grpcServer := grpc.NewServer()
	RegisterAgentPublicServer(grpcServer, s)

	fmt.Printf("gRPC public server listening on %s\n", tcpAddr)
	if err := grpcServer.Serve(lis); err != nil {
		panic(fmt.Sprintf("grpc_public: server failed: %v", err))
	}
}

// GetStatus implements AgentPublicServer.
func (s *AgentGRPCPublicServer) GetStatus(_ context.Context, _ *GetStatusRequest) (*GetStatusResponse, error) {
	return &GetStatusResponse{Status: "ok"}, nil
}
