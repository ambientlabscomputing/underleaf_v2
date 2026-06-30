package service

import (
	"context"
	"fmt"
	"time"

	agentpb "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

type ConnectionService struct {
	cloudClient *clients.CloudClient
	agentClient *clients.AgentClient
}

func NewConnectionService(cc *clients.CloudClient, ac *clients.AgentClient) *ConnectionService {
	return &ConnectionService{
		cloudClient: cc,
		agentClient: ac,
	}
}

// this is more of the admin step to setup the connection and take up a conenction slot
func (s *ConnectionService) CreateConnection(ctx context.Context, nodeID, name string) (*types.Connection, error) {
	req := types.CreateConnectionRequest{
		NodeID: nodeID,
		Name:   name,
	}
	conn, err := s.cloudClient.CreateConnection(ctx, req)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (s *ConnectionService) GetConnection(ctx context.Context, connectionID string) (*types.Connection, error) {
	conn, err := s.cloudClient.GetConnection(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (s *ConnectionService) ListConnections(ctx context.Context, query types.QueryConnectionsRequest) ([]types.Connection, error) {
	connections, err := s.cloudClient.ListConnections(ctx, query)
	if err != nil {
		return nil, err
	}
	return connections, nil
}

func (s *ConnectionService) TerminateConnection(ctx context.Context, connectionID string) error {
	err := s.cloudClient.TerminateConnection(ctx, connectionID)
	if err != nil {
		return err
	}
	return nil
}

func (s *ConnectionService) NewStream(ctx context.Context, nodeID, type_ string, port int) (*types.Stream, error) {
	logger := utils.LoggerFromContext(ctx).With("node_id", nodeID)
	conn, err := s.cloudClient.CreateConnection(ctx, types.CreateConnectionRequest{
		NodeID: nodeID,
		Name:   fmt.Sprintf("stream-%d", time.Now().Unix()),
	})
	if err != nil {
		return nil, err
	}
	logger = logger.With("connection_id", conn.ID)
	logger.Info("connection created, creating new stream")
	stream, err := s.cloudClient.NewStream(ctx, types.NewStreamRequest{
		ConnectionID: conn.ID,
		Type:         types.StreamType(type_),
		Port:         &port,
	})
	if err != nil {
		logger.Error("failed to create new stream", "error", err)
		return nil, err
	}
	logger = logger.With("stream_id", stream.ID)
	logger.Info("new stream provisioned cloud-side")

	if err := s.agentClient.NewStream(ctx, conn.ID, stream.ID, stream.Type.ToString(), *stream.Port); err != nil {
		logger.Error("failed to create new stream on agent", "error", err)
		return nil, err
	}
	logger.Info("requested agent to create new stream")

	streamState, err := s.pollStreamState(ctx, stream.ID, 30*time.Second)
	if err != nil {
		logger.Error("failed to poll stream state", "error", err)
		return nil, err
	}
	logger = logger.With("stream_state", streamState.State)
	if streamState.State != "active" {
		logger.Error("stream did not become active in time")
		return nil, fmt.Errorf("stream did not become active in time")
	}
	logger.Info("stream is now active")
	return stream, nil
}

func (s *ConnectionService) CloseStream(ctx context.Context, streamID string) error {
	logger := utils.LoggerFromContext(ctx).With("stream_id", streamID)
	err := s.cloudClient.CloseStream(ctx, streamID)
	if err != nil {
		logger.Error("failed to close stream", "error", err)
		return err
	}
	logger.Info("stream closed successfully")
	return nil
}

func (s *ConnectionService) GetStream(ctx context.Context, streamID string) (*agentpb.StreamState, error) {
	streamState, err := s.agentClient.GetStream(ctx, streamID)
	if err != nil {
		return nil, err
	}
	return streamState, nil
}

func (s *ConnectionService) ListStreams(ctx context.Context) ([]*agentpb.StreamState, error) {
	// TODO: multi-node support
	streamStates, err := s.agentClient.ListStreams(ctx)
	if err != nil {
		return nil, err
	}
	return streamStates, nil
}

func (s *ConnectionService) pollStreamState(ctx context.Context, streamID string, timeout time.Duration) (*agentpb.StreamState, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			streamState, err := s.agentClient.GetStream(ctx, streamID)
			if err != nil {
				return nil, err
			}
			if streamState.State == "active" {
				return streamState, nil
			}
		case <-timer.C:
			return nil, fmt.Errorf("timeout reached while waiting for stream %s to become active", streamID)
		}
	}
}
