package grpc_public

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
)

//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative orchestrator_public.proto

const tcpAddr = ":50100"

type OrchestratorGRPCPublicServer struct {
	UnimplementedOrchestratorPublicServer
	Service *service.AppService
}

// Serve starts the gRPC public server on TCP for agent communication.
func (s *OrchestratorGRPCPublicServer) Serve() {
	lis, err := net.Listen("tcp", tcpAddr)
	if err != nil {
		panic(fmt.Sprintf("grpc_public: failed to listen on %s: %v", tcpAddr, err))
	}

	grpcServer := grpc.NewServer()
	RegisterOrchestratorPublicServer(grpcServer, s)

	fmt.Printf("gRPC public server listening on %s\n", tcpAddr)
	if err := grpcServer.Serve(lis); err != nil {
		panic(fmt.Sprintf("grpc_public: server failed: %v", err))
	}
}

// GetNodes implements OrchestratorPublicServer.
func (s *OrchestratorGRPCPublicServer) GetNodes(_ context.Context, _ *GetNodesRequest) (*GetNodesResponse, error) {
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
