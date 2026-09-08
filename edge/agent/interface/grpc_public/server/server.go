package server

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	grpc_public "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/lib/conn_client"
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
			Name:     c.Name,
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

func (s *AgentGRPCPublicServer) GetNode(ctx context.Context, _ *emptypb.Empty) (*grpc_public.Node, error) {
	node, err := s.Service.GetNode()
	if err != nil {
		return nil, err
	}
	return &grpc_public.Node{
		Id:        node.ID,
		Name:      node.Name,
		IpAddress: node.IPAddr,
		Os:        node.OS,
		Arch:      node.Arch,
	}, nil
}

// CreateVolume implements AgentPublicServer.
func (s *AgentGRPCPublicServer) CreateVolume(ctx context.Context, req *grpc_public.CreateVolumeRequest) (*grpc_public.CreateVolumeResponse, error) {
	if err := s.Service.Docker().CreateVolume(ctx, req.Name, req.Driver, req.Labels); err != nil {
		return nil, err
	}
	return &grpc_public.CreateVolumeResponse{Name: req.Name}, nil
}

// ListVolumes implements AgentPublicServer.
func (s *AgentGRPCPublicServer) ListVolumes(ctx context.Context, _ *grpc_public.ListVolumesRequest) (*grpc_public.ListVolumesResponse, error) {
	volumes, err := s.Service.Docker().ListVolumes(ctx)
	if err != nil {
		return nil, err
	}
	resp := &grpc_public.ListVolumesResponse{}
	for _, v := range volumes {
		resp.Volumes = append(resp.Volumes, &grpc_public.VolumeInfo{
			Name:   v.Name,
			Driver: v.Driver,
			Labels: v.Labels,
		})
	}
	return resp, nil
}

// CreateContainer implements AgentPublicServer.
func (s *AgentGRPCPublicServer) CreateContainer(ctx context.Context, req *grpc_public.CreateContainerRequest) (*grpc_public.CreateContainerResponse, error) {
	opts := service.ContainerCreateOptions{
		Name:        req.Name,
		Image:       req.Image,
		Environment: req.Environment,
		Ports:       req.Ports,
		Volumes:     req.Volumes,
		Labels:      req.Labels,
	}
	if req.Build != nil {
		opts.Build = &service.BuildSource{
			ArchiveURL: req.Build.ArchiveUrl,
			Context:    req.Build.Context,
			Dockerfile: req.Build.Dockerfile,
			Args:       req.Build.Args,
		}
	}

	dockerID, err := s.Service.Docker().CreateContainer(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &grpc_public.CreateContainerResponse{DockerId: dockerID}, nil
}

// StartContainer implements AgentPublicServer.
func (s *AgentGRPCPublicServer) StartContainer(ctx context.Context, req *grpc_public.ContainerNameRequest) (*emptypb.Empty, error) {
	if err := s.Service.Docker().StartContainer(ctx, req.Name); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// StopContainer implements AgentPublicServer.
func (s *AgentGRPCPublicServer) StopContainer(ctx context.Context, req *grpc_public.ContainerNameRequest) (*emptypb.Empty, error) {
	if err := s.Service.Docker().StopContainer(ctx, req.Name); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// RemoveContainer implements AgentPublicServer.
func (s *AgentGRPCPublicServer) RemoveContainer(ctx context.Context, req *grpc_public.ContainerNameRequest) (*emptypb.Empty, error) {
	if err := s.Service.Docker().RemoveContainer(ctx, req.Name); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// NewStream implements AgentPublicServer — asks the agent to open a new multiplexed stream over its cloud connection.
func (s *AgentGRPCPublicServer) NewStream(ctx context.Context, req *grpc_public.NewStreamRequest) (*emptypb.Empty, error) {
	conns := s.Service.Connections()
	if conns == nil {
		return nil, status.Error(codes.FailedPrecondition, "node has not completed cluster registration; no cloud connection available")
	}
	err := conns.NewStream(ctx, service.NewStreamRequest{
		ConnectionID: req.ConnectionId,
		StreamID:     req.StreamId,
		Type:         req.Type,
		Port:         int(req.Port),
	})
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// CloseStream implements AgentPublicServer.
func (s *AgentGRPCPublicServer) CloseStream(ctx context.Context, req *grpc_public.CloseStreamRequest) (*emptypb.Empty, error) {
	conns := s.Service.Connections()
	if conns == nil {
		return nil, status.Error(codes.FailedPrecondition, "node has not completed cluster registration; no cloud connection available")
	}
	if err := conns.CloseStream(ctx, req.StreamId); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// GetStream implements AgentPublicServer.
func (s *AgentGRPCPublicServer) GetStream(ctx context.Context, req *grpc_public.GetStreamRequest) (*grpc_public.StreamState, error) {
	conns := s.Service.Connections()
	if conns == nil {
		return nil, status.Error(codes.FailedPrecondition, "node has not completed cluster registration; no cloud connection available")
	}
	streamState, ok := conns.GetStream(ctx, req.StreamId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "stream %s not found", req.StreamId)
	}
	return streamStateToProto(streamState), nil
}

// ListStreams implements AgentPublicServer.
func (s *AgentGRPCPublicServer) ListStreams(_ *emptypb.Empty, stream grpc_public.AgentPublic_ListStreamsServer) error {
	conns := s.Service.Connections()
	if conns == nil {
		return status.Error(codes.FailedPrecondition, "node has not completed cluster registration; no cloud connection available")
	}
	for _, streamState := range conns.ListStreams(stream.Context()) {
		if err := stream.Send(streamStateToProto(streamState)); err != nil {
			return err
		}
	}
	return nil
}

func streamStateToProto(s conn_client.StreamState) *grpc_public.StreamState {
	return &grpc_public.StreamState{
		StreamId:     s.StreamID,
		Type:         string(s.Type),
		ConnectionId: s.ConnectionID,
		State:        string(s.State),
	}
}
