package link

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	agentpb "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/lib/conn_client"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/repository"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// Metadata keys of the LogStream sub-stream (kept in sync with
// edge/orchestrator/link; duplicated so neither side imports the other).
const (
	metaStreamToken = "x-link-stream-token"
	metaNodeID      = "x-link-node-id"
)

// Dispatcher is the command vocabulary: each case is one of the former
// AgentPublic RPCs, now carried over the link instead of a listening port.
type Dispatcher struct {
	Service *service.AppService
}

var _ Handler = (*Dispatcher)(nil)

func noConnections() error {
	return status.Error(codes.FailedPrecondition, "node has not completed cluster registration; no cloud connection available")
}

// Handle implements Handler.
func (d *Dispatcher) Handle(ctx context.Context, cmd *agentpb.Command, env Env) *agentpb.CommandResult {
	id := cmd.GetRequestId()
	res, err := d.handle(ctx, cmd, env)
	if err != nil {
		return errResult(id, err)
	}
	res.RequestId = id
	return res
}

func (d *Dispatcher) handle(ctx context.Context, cmd *agentpb.Command, env Env) (*agentpb.CommandResult, error) {
	svc := d.Service
	switch c := cmd.GetCmd().(type) {
	case *agentpb.Command_Ping:
		r, err := svc.Health().Ping(c.Ping.GetSource())
		if err != nil {
			return nil, err
		}
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_Ping{Ping: &agentpb.PingResponse{
			Responder: r.Responder, TimestampUnixMs: r.TimestampUnixMs,
		}}}, nil

	case *agentpb.Command_GetStatus:
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_GetStatus{GetStatus: &agentpb.GetStatusResponse{Status: "ok"}}}, nil

	case *agentpb.Command_IngestContainers:
		containers, err := svc.Docker().IngestContainers(ctx)
		if err != nil {
			return nil, err
		}
		resp := &agentpb.IngestContainersResponse{}
		for _, ct := range containers {
			resp.Containers = append(resp.Containers, &agentpb.Container{
				Id: ct.ID, DockerId: ct.DockerID, NodeId: string(ct.NodeID),
				Image: ct.Image, Status: ct.Status, Uptime: ct.Uptime, Name: ct.Name,
			})
		}
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_IngestContainers{IngestContainers: resp}}, nil

	case *agentpb.Command_GetNode:
		n, err := svc.GetNode()
		if err != nil {
			return nil, err
		}
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_Node{Node: &agentpb.Node{
			Id: n.ID, Name: n.Name, IpAddress: n.IPAddr, Os: n.OS, Arch: n.Arch,
		}}}, nil

	case *agentpb.Command_NewStream:
		conns := svc.Connections()
		if conns == nil {
			return nil, noConnections()
		}
		r := c.NewStream
		if err := conns.NewStream(ctx, service.NewStreamRequest{
			ConnectionID: r.GetConnectionId(), StreamID: r.GetStreamId(), Type: r.GetType(), Port: int(r.GetPort()),
		}); err != nil {
			return nil, err
		}
		return okResult(), nil

	case *agentpb.Command_CloseStream:
		conns := svc.Connections()
		if conns == nil {
			return nil, noConnections()
		}
		if err := conns.CloseStream(ctx, c.CloseStream.GetStreamId()); err != nil {
			return nil, err
		}
		return okResult(), nil

	case *agentpb.Command_GetStream:
		conns := svc.Connections()
		if conns == nil {
			return nil, noConnections()
		}
		st, ok := conns.GetStream(ctx, c.GetStream.GetStreamId())
		if !ok {
			return nil, status.Errorf(codes.NotFound, "stream %s not found", c.GetStream.GetStreamId())
		}
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_Stream{Stream: streamStateToProto(st)}}, nil

	case *agentpb.Command_ListStreams:
		conns := svc.Connections()
		if conns == nil {
			return nil, noConnections()
		}
		out := &agentpb.ListStreamsResult{}
		for _, st := range conns.ListStreams(ctx) {
			out.Streams = append(out.Streams, streamStateToProto(st))
		}
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_Streams{Streams: out}}, nil

	case *agentpb.Command_GetContainerLogs:
		r := c.GetContainerLogs
		page, err := svc.Logs().GetLogs(repository.QueryLogsParams{
			DockerID: r.DockerId, SinceMs: r.SinceMs, UntilMs: r.UntilMs, Limit: int(r.Limit), CursorID: r.CursorId,
		})
		if err != nil {
			return nil, err
		}
		resp := &agentpb.GetContainerLogsResponse{NextCursor: page.NextCursor}
		for _, l := range page.Lines {
			resp.Lines = append(resp.Lines, &agentpb.LogLine{DockerId: l.DockerID, TsMs: l.TsMs, Stream: l.Stream, Message: l.Message})
		}
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_ContainerLogs{ContainerLogs: resp}}, nil

	case *agentpb.Command_CreateVolume:
		r := c.CreateVolume
		if err := svc.Docker().CreateVolume(ctx, r.Name, r.Driver, r.Labels); err != nil {
			return nil, err
		}
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_CreateVolume{CreateVolume: &agentpb.CreateVolumeResponse{Name: r.Name}}}, nil

	case *agentpb.Command_ListVolumes:
		volumes, err := svc.Docker().ListVolumes(ctx)
		if err != nil {
			return nil, err
		}
		resp := &agentpb.ListVolumesResponse{}
		for _, v := range volumes {
			resp.Volumes = append(resp.Volumes, &agentpb.VolumeInfo{Name: v.Name, Driver: v.Driver, Labels: v.Labels})
		}
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_Volumes{Volumes: resp}}, nil

	case *agentpb.Command_CreateContainer:
		r := c.CreateContainer
		opts := service.ContainerCreateOptions{
			Name: r.Name, Image: r.Image, Environment: r.Environment, Ports: r.Ports, Volumes: r.Volumes, Labels: r.Labels,
		}
		if r.Build != nil {
			opts.Build = &service.BuildSource{
				ArchiveURL: r.Build.ArchiveUrl, Context: r.Build.Context, Dockerfile: r.Build.Dockerfile, Args: r.Build.Args,
			}
		}
		dockerID, err := svc.Docker().CreateContainer(ctx, opts)
		if err != nil {
			return nil, err
		}
		return &agentpb.CommandResult{Result: &agentpb.CommandResult_CreateContainer{CreateContainer: &agentpb.CreateContainerResponse{DockerId: dockerID}}}, nil

	case *agentpb.Command_StartContainer:
		if err := svc.Docker().StartContainer(ctx, c.StartContainer.GetName()); err != nil {
			return nil, err
		}
		return okResult(), nil

	case *agentpb.Command_StopContainer:
		if err := svc.Docker().StopContainer(ctx, c.StopContainer.GetName()); err != nil {
			return nil, err
		}
		return okResult(), nil

	case *agentpb.Command_RemoveContainer:
		if err := svc.Docker().RemoveContainer(ctx, c.RemoveContainer.GetName()); err != nil {
			return nil, err
		}
		return okResult(), nil

	case *agentpb.Command_OpenLogStream:
		r := c.OpenLogStream
		if r.GetStreamToken() == "" || r.GetDockerId() == "" {
			return nil, status.Error(codes.InvalidArgument, "open_log_stream needs a token and a docker id")
		}
		// The command is answered at once; the stream lives on its own gRPC
		// stream until either side ends it, and never outlives the link.
		go d.streamLogs(env, r)
		return okResult(), nil

	default:
		return nil, status.Errorf(codes.Unimplemented, "this agent does not know the command %T", cmd.GetCmd())
	}
}

func okResult() *agentpb.CommandResult {
	return &agentpb.CommandResult{Result: &agentpb.CommandResult_Ok{Ok: &emptypb.Empty{}}}
}

func streamStateToProto(s conn_client.StreamState) *agentpb.StreamState {
	return &agentpb.StreamState{StreamId: s.StreamID, Type: string(s.Type), ConnectionId: s.ConnectionID, State: string(s.State)}
}

// streamLogs is the former StreamContainerLogs body, writing to a LogStream
// sub-stream: replay history if asked, then follow live lines until the
// Orchestrator ends the stream or the link drops.
func (d *Dispatcher) streamLogs(env Env, r *agentpb.OpenLogStream) {
	md := metadata.Pairs(metaStreamToken, r.GetStreamToken())
	if env.Local {
		md.Append(metaNodeID, env.NodeID)
	}
	ctx, cancel := context.WithCancel(metadata.NewOutgoingContext(env.LinkCtx, md))
	defer cancel()

	stream, err := agentpb.NewAgentLinkClient(env.Conn).LogStream(ctx)
	if err != nil {
		return
	}
	// The Orchestrator never sends on this stream; Recv returns when it ends
	// the call, which is how an idle stream learns it was cancelled.
	go func() {
		_, _ = stream.Recv()
		cancel()
	}()
	defer stream.CloseSend()

	dockerID := r.GetDockerId()
	logs := d.Service.Logs()
	// Subscribe before reading history so no line falls in the gap.
	live := logs.Subscribe(dockerID)
	defer logs.Unsubscribe(dockerID, live)

	send := func(l *types.LogLine) error {
		return stream.Send(&agentpb.LogLine{DockerId: l.DockerID, TsMs: l.TsMs, Stream: l.Stream, Message: l.Message})
	}

	if r.GetSinceMs() > 0 {
		cursor := int64(0)
		for {
			page, err := logs.GetLogs(repository.QueryLogsParams{DockerID: dockerID, SinceMs: r.GetSinceMs(), CursorID: cursor})
			if err != nil {
				return
			}
			for _, l := range page.Lines {
				if err := send(l); err != nil {
					return
				}
			}
			if page.NextCursor == 0 {
				break
			}
			cursor = page.NextCursor
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-live:
			if !ok {
				return
			}
			if err := send(line); err != nil {
				return
			}
		}
	}
}
