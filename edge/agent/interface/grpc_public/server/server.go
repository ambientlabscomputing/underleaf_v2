package server

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"

	grpc_public "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/repository"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/service"
)

const tcpAddr = ":50101"

type AgentGRPCPublicServer struct {
	grpc_public.UnimplementedAgentPublicServer
	Service *service.AppService
}

// Serve starts the gRPC public server on TCP for orchestrator communication.
func (s *AgentGRPCPublicServer) Serve() {
	lis, err := net.Listen("tcp", tcpAddr)
	if err != nil {
		panic(fmt.Sprintf("grpc_public: failed to listen on %s: %v", tcpAddr, err))
	}

	grpcServer := grpc.NewServer()
	grpc_public.RegisterAgentPublicServer(grpcServer, s)

	fmt.Printf("gRPC public server listening on %s\n", tcpAddr)
	if err := grpcServer.Serve(lis); err != nil {
		panic(fmt.Sprintf("grpc_public: server failed: %v", err))
	}
}

// GetStatus implements AgentPublicServer.
func (s *AgentGRPCPublicServer) GetStatus(_ context.Context, _ *grpc_public.GetStatusRequest) (*grpc_public.GetStatusResponse, error) {
	return &grpc_public.GetStatusResponse{Status: "ok"}, nil
}

// Ping implements AgentPublicServer — delegates to the Health service.
func (s *AgentGRPCPublicServer) Ping(_ context.Context, req *grpc_public.PingRequest) (*grpc_public.PingResponse, error) {
	result, err := s.Service.Health().Ping(req.Source)
	if err != nil {
		return nil, err
	}
	return &grpc_public.PingResponse{
		Responder:       result.Responder,
		TimestampUnixMs: result.TimestampUnixMs,
	}, nil
}

// IngestContainers implements AgentPublicServer — allows the orchestrator to trigger container ingestion.
func (s *AgentGRPCPublicServer) IngestContainers(ctx context.Context, _ *grpc_public.IngestContainersRequest) (*grpc_public.IngestContainersResponse, error) {
	containers, err := s.Service.Docker().IngestContainers(ctx)
	if err != nil {
		return nil, err
	}
	resp := &grpc_public.IngestContainersResponse{}
	for _, c := range containers {
		resp.Containers = append(resp.Containers, &grpc_public.Container{
			Id:       c.ID,
			DockerId: c.DockerID,
			NodeId:   string(c.NodeID),
			Image:    c.Image,
			Status:   c.Status,
			Uptime:   c.Uptime,
		})
	}
	return resp, nil
}

// GetContainerLogs implements AgentPublicServer — returns a page of persisted log lines.
func (s *AgentGRPCPublicServer) GetContainerLogs(_ context.Context, req *grpc_public.GetContainerLogsRequest) (*grpc_public.GetContainerLogsResponse, error) {
	page, err := s.Service.Logs().GetLogs(repository.QueryLogsParams{
		DockerID: req.DockerId,
		SinceMs:  req.SinceMs,
		UntilMs:  req.UntilMs,
		Limit:    int(req.Limit),
		CursorID: req.CursorId,
	})
	if err != nil {
		return nil, err
	}
	resp := &grpc_public.GetContainerLogsResponse{NextCursor: page.NextCursor}
	for _, l := range page.Lines {
		resp.Lines = append(resp.Lines, &grpc_public.LogLine{
			DockerId: l.DockerID,
			TsMs:     l.TsMs,
			Stream:   l.Stream,
			Message:  l.Message,
		})
	}
	return resp, nil
}

// StreamContainerLogs implements AgentPublicServer — replays history then streams live lines.
func (s *AgentGRPCPublicServer) StreamContainerLogs(req *grpc_public.StreamContainerLogsRequest, stream grpc_public.AgentPublic_StreamContainerLogsServer) error {
	dockerID := req.DockerId
	ctx := stream.Context()

	// Subscribe to live fan-out before querying history to avoid a gap.
	liveCh := s.Service.Logs().Subscribe(dockerID)
	defer s.Service.Logs().Unsubscribe(dockerID, liveCh)

	// Replay persisted history if requested.
	if req.SinceMs > 0 {
		cursor := int64(0)
		for {
			page, err := s.Service.Logs().GetLogs(repository.QueryLogsParams{
				DockerID: dockerID,
				SinceMs:  req.SinceMs,
				CursorID: cursor,
			})
			if err != nil {
				return err
			}
			for _, l := range page.Lines {
				if err := stream.Send(&grpc_public.LogLine{
					DockerId: l.DockerID,
					TsMs:     l.TsMs,
					Stream:   l.Stream,
					Message:  l.Message,
				}); err != nil {
					return err
				}
			}
			if page.NextCursor == 0 {
				break
			}
			cursor = page.NextCursor
		}
	}

	// Stream live lines until the client disconnects.
	for {
		select {
		case <-ctx.Done():
			return nil
		case line, ok := <-liveCh:
			if !ok {
				return nil
			}
			if err := stream.Send(&grpc_public.LogLine{
				DockerId: line.DockerID,
				TsMs:     line.TsMs,
				Stream:   line.Stream,
				Message:  line.Message,
			}); err != nil {
				return err
			}
		}
	}
}
