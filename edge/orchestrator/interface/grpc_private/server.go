package grpc_private

import (
	"context"
	"fmt"
	"net"
	"os"

	"google.golang.org/grpc"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
)

//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative orchestrator_private.proto

const unixSocketPath = "/tmp/undf-orch.sock"

type OrchestratorGRPCPrivateServer struct {
	UnimplementedOrchestratorPrivateServer
	Service *service.AppService
}

// Serve starts the gRPC private server on a unix socket for CLI access.
func (s *OrchestratorGRPCPrivateServer) Serve() {
	if err := os.Remove(unixSocketPath); err != nil && !os.IsNotExist(err) {
		panic(fmt.Sprintf("grpc_private: failed to remove stale socket: %v", err))
	}

	lis, err := net.Listen("unix", unixSocketPath)
	if err != nil {
		panic(fmt.Sprintf("grpc_private: failed to listen on unix socket: %v", err))
	}

	grpcServer := grpc.NewServer()
	RegisterOrchestratorPrivateServer(grpcServer, s)

	fmt.Printf("gRPC private server listening on unix://%s\n", unixSocketPath)
	if err := grpcServer.Serve(lis); err != nil {
		panic(fmt.Sprintf("grpc_private: server failed: %v", err))
	}
}

// GetNodes implements OrchestratorPrivateServer.
func (s *OrchestratorGRPCPrivateServer) GetNodes(_ context.Context, _ *GetNodesRequest) (*GetNodesResponse, error) {
	nodes, err := s.Service.Nodes().GetNodes()
	if err != nil {
		return nil, err
	}

	resp := &GetNodesResponse{}
	for _, n := range nodes {
		resp.Nodes = append(resp.Nodes, &Node{Id: n.ID, Name: n.Name})
	}
	return resp, nil
}
