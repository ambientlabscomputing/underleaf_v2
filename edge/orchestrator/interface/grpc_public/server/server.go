package server

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"

	grpc_public "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	sharedtypes "github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
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
func (s *OrchestratorGRPCPublicServer) GetNodes(_ context.Context, req *grpc_public.GetNodesRequest) (*grpc_public.GetNodesResponse, error) {
	resp, err := s.Service.Nodes().GetNodes(sharedtypes.QueryNodesRequest{
		BaseQueryRequest: sharedtypes.BaseQueryRequest{
			Limit:   utils.Int64PtrToInt(req.Limit),
			Offset:  utils.Int64PtrToInt(req.Offset),
			Order:   utils.ParseStringPtr(req.Order),
			OrderBy: utils.ParseStringPtr(req.OrderBy),
		},
		Name:   utils.ParseStringPtr(req.Name),
		OS:     utils.ParseStringPtr(req.Os),
		Arch:   utils.ParseStringPtr(req.Arch),
		Search: utils.ParseStringPtr(req.Search),
	})
	if err != nil {
		return nil, err
	}

	nodesResp := &grpc_public.GetNodesResponse{
		Total: *utils.IntToInt64Ptr(resp.Total),
		Query: req,
	}
	count := 0
	for _, n := range resp.Results {
		nodesResp.Results = append(nodesResp.Results, &grpc_public.Node{
			Id:        n.ID,
			Name:      n.Name,
			IpAddress: n.IPAddr,
			Os:        n.OS,
			Arch:      n.Arch,
		})
		count++
	}
	nodesResp.Count = int64(count)
	return nodesResp, nil
}

// CreateNode implements OrchestratorPublicServer.
func (s *OrchestratorGRPCPublicServer) CreateNode(ctx context.Context, req *grpc_public.CreateNodeRequest) (*grpc_public.CreateNodeResponse, error) {
	node, err := s.Service.Nodes().CreateNode(sharedtypes.CreateNodeRequest{
		Name:   req.Name,
		IPAddr: req.IpAddress,
		OS:     req.Os,
		Arch:   req.Arch,
	})
	if err != nil {
		return nil, err
	}
	return &grpc_public.CreateNodeResponse{
		Id:        node.ID,
		Name:      node.Name,
		IpAddress: node.IPAddr,
		Os:        node.OS,
		Arch:      node.Arch,
	}, nil
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
