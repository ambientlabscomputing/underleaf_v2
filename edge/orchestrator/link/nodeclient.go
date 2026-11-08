package link

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	agentpb "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
)

// nodeClient is clients.AgentAPI for one node: every method is a command sent
// down that node's link.
type nodeClient struct {
	hub    *Hub
	nodeID string
}

func (c *nodeClient) call(ctx context.Context, cmd *agentpb.Command) (*agentpb.CommandResult, error) {
	return c.hub.Call(ctx, c.nodeID, cmd)
}

func unexpected(what string, res *agentpb.CommandResult) error {
	return status.Errorf(codes.Internal, "agent answered %s with an unexpected result %T", what, res.GetResult())
}

func (c *nodeClient) Ping(ctx context.Context, source string) (*clients.PingResult, error) {
	res, err := c.call(ctx, &agentpb.Command{Cmd: &agentpb.Command_Ping{Ping: &agentpb.PingRequest{
		Source: source, TimestampUnixMs: time.Now().UnixMilli(),
	}}})
	if err != nil {
		return nil, err
	}
	p := res.GetPing()
	if p == nil {
		return nil, unexpected("ping", res)
	}
	return &clients.PingResult{Responder: p.Responder, TimestampUnixMs: p.TimestampUnixMs}, nil
}

func (c *nodeClient) IngestContainers(ctx context.Context) (int, error) {
	res, err := c.call(ctx, &agentpb.Command{Cmd: &agentpb.Command_IngestContainers{IngestContainers: &agentpb.IngestContainersRequest{}}})
	if err != nil {
		return 0, err
	}
	r := res.GetIngestContainers()
	if r == nil {
		return 0, unexpected("ingest_containers", res)
	}
	return len(r.GetContainers()), nil
}

func (c *nodeClient) GetContainerLogs(ctx context.Context, dockerID string, sinceMs, untilMs int64, limit int, cursorID int64) (*agentpb.GetContainerLogsResponse, error) {
	res, err := c.call(ctx, &agentpb.Command{Cmd: &agentpb.Command_GetContainerLogs{GetContainerLogs: &agentpb.GetContainerLogsRequest{
		DockerId: dockerID, SinceMs: sinceMs, UntilMs: untilMs, Limit: int32(limit), CursorId: cursorID,
	}}})
	if err != nil {
		return nil, err
	}
	r := res.GetContainerLogs()
	if r == nil {
		return nil, unexpected("get_container_logs", res)
	}
	return r, nil
}

// StreamContainerLogs follows a container's logs through a sub-stream: it
// issues a single-use token, tells the agent to open the sub-stream, and waits
// for it to attach. Cancelling ctx ends the stream.
func (c *nodeClient) StreamContainerLogs(ctx context.Context, dockerID string, sinceMs int64) (<-chan *agentpb.LogLine, error) {
	tok, ls := c.hub.expectStream(c.nodeID)
	fail := func(err error) (<-chan *agentpb.LogLine, error) {
		c.hub.forgetStream(tok)
		ls.cancel()
		ls.closeLines()
		return nil, err
	}
	res, err := c.call(ctx, &agentpb.Command{Cmd: &agentpb.Command_OpenLogStream{OpenLogStream: &agentpb.OpenLogStream{
		StreamToken: tok, DockerId: dockerID, SinceMs: sinceMs,
	}}})
	if err != nil {
		return fail(err)
	}
	if res.GetOk() == nil {
		return fail(unexpected("open_log_stream", res))
	}
	select {
	case <-ls.attached:
	case <-time.After(streamAttachTimeout):
		return fail(fmt.Errorf("node %s accepted the log stream but never opened it", c.nodeID))
	case <-ctx.Done():
		return fail(ctx.Err())
	}
	go func() {
		<-ctx.Done()
		ls.cancel()
	}()
	return ls.lines, nil
}

func (c *nodeClient) GetNode(ctx context.Context) (*agentpb.Node, error) {
	res, err := c.call(ctx, &agentpb.Command{Cmd: &agentpb.Command_GetNode{GetNode: &emptypb.Empty{}}})
	if err != nil {
		return nil, err
	}
	n := res.GetNode()
	if n == nil {
		return nil, unexpected("get_node", res)
	}
	return n, nil
}

func (c *nodeClient) okCall(ctx context.Context, what string, cmd *agentpb.Command) error {
	res, err := c.call(ctx, cmd)
	if err != nil {
		return err
	}
	if res.GetOk() == nil {
		return unexpected(what, res)
	}
	return nil
}

func (c *nodeClient) NewStream(ctx context.Context, connectionID, streamID, streamType string, port int) error {
	return c.okCall(ctx, "new_stream", &agentpb.Command{Cmd: &agentpb.Command_NewStream{NewStream: &agentpb.NewStreamRequest{
		ConnectionId: connectionID, StreamId: streamID, Type: streamType, Port: int32(port),
	}}})
}

func (c *nodeClient) SetRemoteConnection(ctx context.Context, connectionID string) error {
	return c.okCall(ctx, "set_remote_connection", &agentpb.Command{Cmd: &agentpb.Command_SetRemoteConnection{SetRemoteConnection: &agentpb.SetRemoteConnection{
		ConnectionId: connectionID,
	}}})
}

func (c *nodeClient) CloseStream(ctx context.Context, streamID string) error {
	return c.okCall(ctx, "close_stream", &agentpb.Command{Cmd: &agentpb.Command_CloseStream{CloseStream: &agentpb.CloseStreamRequest{StreamId: streamID}}})
}

func (c *nodeClient) GetStream(ctx context.Context, streamID string) (*agentpb.StreamState, error) {
	res, err := c.call(ctx, &agentpb.Command{Cmd: &agentpb.Command_GetStream{GetStream: &agentpb.GetStreamRequest{StreamId: streamID}}})
	if err != nil {
		return nil, err
	}
	s := res.GetStream()
	if s == nil {
		return nil, unexpected("get_stream", res)
	}
	return s, nil
}

func (c *nodeClient) ListStreams(ctx context.Context) ([]*agentpb.StreamState, error) {
	res, err := c.call(ctx, &agentpb.Command{Cmd: &agentpb.Command_ListStreams{ListStreams: &emptypb.Empty{}}})
	if err != nil {
		return nil, err
	}
	l := res.GetStreams()
	if l == nil {
		return nil, unexpected("list_streams", res)
	}
	return l.GetStreams(), nil
}

func (c *nodeClient) CreateVolume(ctx context.Context, name, driver string, labels map[string]string) error {
	res, err := c.call(ctx, &agentpb.Command{Cmd: &agentpb.Command_CreateVolume{CreateVolume: &agentpb.CreateVolumeRequest{
		Name: name, Driver: driver, Labels: labels,
	}}})
	if err != nil {
		return err
	}
	if res.GetCreateVolume() == nil {
		return unexpected("create_volume", res)
	}
	return nil
}

func (c *nodeClient) ListVolumes(ctx context.Context) ([]*agentpb.VolumeInfo, error) {
	res, err := c.call(ctx, &agentpb.Command{Cmd: &agentpb.Command_ListVolumes{ListVolumes: &agentpb.ListVolumesRequest{}}})
	if err != nil {
		return nil, err
	}
	v := res.GetVolumes()
	if v == nil {
		return nil, unexpected("list_volumes", res)
	}
	return v.GetVolumes(), nil
}

func (c *nodeClient) CreateContainer(ctx context.Context, req clients.CreateContainerRequest) (string, error) {
	pb := &agentpb.CreateContainerRequest{
		Name:        req.Name,
		Image:       req.Image,
		Environment: req.Environment,
		Ports:       req.Ports,
		Volumes:     req.Volumes,
		Labels:      req.Labels,
	}
	if req.Build != nil {
		pb.Build = &agentpb.BuildSource{
			ArchiveUrl: req.Build.ArchiveURL,
			Context:    req.Build.Context,
			Dockerfile: req.Build.Dockerfile,
			Args:       req.Build.Args,
		}
	}
	res, err := c.call(ctx, &agentpb.Command{Cmd: &agentpb.Command_CreateContainer{CreateContainer: pb}})
	if err != nil {
		return "", err
	}
	r := res.GetCreateContainer()
	if r == nil {
		return "", unexpected("create_container", res)
	}
	return r.GetDockerId(), nil
}

func (c *nodeClient) StartContainer(ctx context.Context, name string) error {
	return c.okCall(ctx, "start_container", &agentpb.Command{Cmd: &agentpb.Command_StartContainer{StartContainer: &agentpb.ContainerNameRequest{Name: name}}})
}

func (c *nodeClient) StopContainer(ctx context.Context, name string) error {
	return c.okCall(ctx, "stop_container", &agentpb.Command{Cmd: &agentpb.Command_StopContainer{StopContainer: &agentpb.ContainerNameRequest{Name: name}}})
}

func (c *nodeClient) RemoveContainer(ctx context.Context, name string) error {
	return c.okCall(ctx, "remove_container", &agentpb.Command{Cmd: &agentpb.Command_RemoveContainer{RemoveContainer: &agentpb.ContainerNameRequest{Name: name}}})
}
