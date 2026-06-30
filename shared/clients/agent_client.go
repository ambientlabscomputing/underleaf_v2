package clients

import (
	"context"
	"io"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

	agentpb "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
)

// TODO: we need to grab this dynamically from the db, ok for now with one node only mode
const agentAddr = "localhost:50101"

// AgentClient is a gRPC client for the AgentPublic service.
type AgentClient struct {
	client agentpb.AgentPublicClient
	conn   *grpc.ClientConn
}

func NewAgentClient() (*AgentClient, error) {
	conn, err := grpc.NewClient(agentAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &AgentClient{
		client: agentpb.NewAgentPublicClient(conn),
		conn:   conn,
	}, nil
}

// Ping calls the agent's Ping RPC and returns the result.
func (c *AgentClient) Ping(ctx context.Context, source string) (*PingResult, error) {
	resp, err := c.client.Ping(ctx, &agentpb.PingRequest{
		Source:          source,
		TimestampUnixMs: time.Now().UnixMilli(),
	})
	if err != nil {
		return nil, err
	}
	return &PingResult{Responder: resp.Responder, TimestampUnixMs: resp.TimestampUnixMs}, nil
}

// Close releases the underlying gRPC connection.
func (c *AgentClient) Close() {
	c.conn.Close()
}

// IngestContainers tells the agent to ingest its local Docker containers and sync them to the orchestrator.
func (c *AgentClient) IngestContainers(ctx context.Context) (int, error) {
	resp, err := c.client.IngestContainers(ctx, &agentpb.IngestContainersRequest{})
	if err != nil {
		return 0, err
	}
	return len(resp.GetContainers()), nil
}

// GetContainerLogs fetches a page of persisted log lines from the agent.
func (c *AgentClient) GetContainerLogs(ctx context.Context, dockerID string, sinceMs, untilMs int64, limit int, cursorID int64) (*agentpb.GetContainerLogsResponse, error) {
	return c.client.GetContainerLogs(ctx, &agentpb.GetContainerLogsRequest{
		DockerId: dockerID,
		SinceMs:  sinceMs,
		UntilMs:  untilMs,
		Limit:    int32(limit),
		CursorId: cursorID,
	})
}

// StreamContainerLogs opens a server-streaming RPC that replays history (if sinceMs > 0)
// then follows live log lines. The returned stream must be closed by the caller.
func (c *AgentClient) StreamContainerLogs(ctx context.Context, dockerID string, sinceMs int64) (agentpb.AgentPublic_StreamContainerLogsClient, error) {
	return c.client.StreamContainerLogs(ctx, &agentpb.StreamContainerLogsRequest{
		DockerId: dockerID,
		SinceMs:  sinceMs,
	})
}

func (c *AgentClient) GetNode(ctx context.Context) (*agentpb.Node, error) {
	resp, err := c.client.GetNode(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// NewStream tells the agent to create a new stream for a given connection.
func (c *AgentClient) NewStream(ctx context.Context, connectionID, streamID, streamType string, port int) error {
	_, err := c.client.NewStream(ctx, &agentpb.NewStreamRequest{
		ConnectionId: connectionID,
		StreamId:     streamID,
		Type:         streamType,
		Port:         int32(port),
	})
	return err
}

// CloseStream tells the agent to close a stream for a given connection.
func (c *AgentClient) CloseStream(ctx context.Context, streamID string) error {
	_, err := c.client.CloseStream(ctx, &agentpb.CloseStreamRequest{
		StreamId: streamID,
	})
	return err
}

// GetStream fetches the state of a stream from the agent.
func (c *AgentClient) GetStream(ctx context.Context, streamID string) (*agentpb.StreamState, error) {
	return c.client.GetStream(ctx, &agentpb.GetStreamRequest{
		StreamId: streamID,
	})
}

// ListStreams fetches the list of streams from the agent.
func (c *AgentClient) ListStreams(ctx context.Context) ([]*agentpb.StreamState, error) {
	stream, err := c.client.ListStreams(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	var streams []*agentpb.StreamState
	for {
		s, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		streams = append(streams, s)
	}
	return streams, nil
}
