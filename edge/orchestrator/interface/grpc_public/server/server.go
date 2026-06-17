package server

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"

	grpc_public "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	sharedtypes "github.com/ambientlabscomputing/underleaf_v2/edge/shared/types"
)

const tcpAddr = ":50100"

type OrchestratorGRPCPublicServer struct {
	grpc_public.UnimplementedOrchestratorPublicServer
	Service *service.AppService
}

// Serve starts the gRPC public server on TCP for agent communication.
func (s *OrchestratorGRPCPublicServer) Serve() {
	lis, err := net.Listen("tcp", tcpAddr)
	if err != nil {
		panic(fmt.Sprintf("grpc_public: failed to listen on %s: %v", tcpAddr, err))
	}

	grpcServer := grpc.NewServer()
	grpc_public.RegisterOrchestratorPublicServer(grpcServer, s)

	fmt.Printf("gRPC public server listening on %s\n", tcpAddr)
	if err := grpcServer.Serve(lis); err != nil {
		panic(fmt.Sprintf("grpc_public: server failed: %v", err))
	}
}

// GetNodes implements OrchestratorPublicServer.
func (s *OrchestratorGRPCPublicServer) GetNodes(_ context.Context, _ *grpc_public.GetNodesRequest) (*grpc_public.GetNodesResponse, error) {
	nodes, err := s.Service.Nodes().GetNodes()
	if err != nil {
		return nil, err
	}

	resp := &grpc_public.GetNodesResponse{}
	for _, n := range nodes {
		resp.Nodes = append(resp.Nodes, &grpc_public.Node{Id: n.ID, Name: n.Name})
	}
	return resp, nil
}

// CreateNode implements OrchestratorPublicServer.
func (s *OrchestratorGRPCPublicServer) CreateNode(ctx context.Context, req *grpc_public.CreateNodeRequest) (*grpc_public.CreateNodeResponse, error) {
	node, err := s.Service.Nodes().CreateNode(service.CreateNodeRequest{Name: req.Name})
	if err != nil {
		return nil, err
	}
	return &grpc_public.CreateNodeResponse{Name: node.Name, Id: node.ID}, nil
}

// Ping implements OrchestratorPublicServer — delegates to the Health service.
func (s *OrchestratorGRPCPublicServer) Ping(_ context.Context, req *grpc_public.PingRequest) (*grpc_public.PingResponse, error) {
	result, err := s.Service.Health().Ping(req.Source)
	if err != nil {
		return nil, err
	}
	return &grpc_public.PingResponse{
		Responder:       result.Responder,
		TimestampUnixMs: result.TimestampUnixMs,
	}, nil
}

// ReportContainers implements OrchestratorPublicServer — receives container state pushed by an agent.
func (s *OrchestratorGRPCPublicServer) ReportContainers(_ context.Context, req *grpc_public.ReportContainersRequest) (*grpc_public.ReportContainersResponse, error) {
	containers := make([]*sharedtypes.Container, 0, len(req.GetContainers()))
	for _, c := range req.GetContainers() {
		containers = append(containers, &sharedtypes.Container{
			ContainerSpec: sharedtypes.ContainerSpec{Image: c.GetImage()},
			ID:            c.GetId(),
			DockerID:      c.GetDockerId(),
			NodeID:        sharedtypes.ForeignKey(c.GetNodeId()),
			Status:        c.GetStatus(),
			Uptime:        c.GetUptime(),
		})
	}
	if err := s.Service.Containers().ReportContainers(req.GetNodeId(), containers); err != nil {
		return nil, err
	}
	return &grpc_public.ReportContainersResponse{Accepted: int32(len(containers))}, nil
}
