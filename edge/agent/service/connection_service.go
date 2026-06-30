package service

import (
	"context"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/lib/conn_client"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// ConnectionService is the agent's entrypoint for interacting with the cloud connection service.
type ConnectionService struct {
	connClient *conn_client.ConnClient
	config     utils.Config
}

func NewConnectionService(config utils.Config, cc *conn_client.ConnClient) *ConnectionService {
	return &ConnectionService{
		connClient: cc,
		config:     config,
	}
}

func (s *ConnectionService) Start() {
	s.connClient.Start()
}

func (s *ConnectionService) ConnectToCloud(ctx context.Context, connectionID string) {
	s.connClient.Connect(ctx, connectionID)
}

func (s *ConnectionService) CloseConnection(ctx context.Context) error {
	return s.connClient.CloseConnection(ctx)
}

func (s *ConnectionService) GetStream(ctx context.Context, streamID string) (conn_client.StreamState, bool) {
	return s.connClient.GetStream(streamID)
}

func (s *ConnectionService) ListStreams(ctx context.Context) []conn_client.StreamState {
	return s.connClient.ListStreams()
}

type NewStreamRequest struct {
	ConnectionID string `json:"connection_id"`
	StreamID     string `json:"stream_id"`
	Type         string `json:"type"`
	Port         int    `json:"port"`
	// Add more fields as needed for new stream types
}

func (s *ConnectionService) NewStream(ctx context.Context, req NewStreamRequest) error {
	s.ConnectToCloud(ctx, req.ConnectionID)
	switch req.Type {
	case "gateway":
		_, err := s.connClient.NewGatewayStream(
			ctx,
			req.StreamID,
			types.StreamType(req.Type),
			req.Port,
		)
		if err != nil {
			return fmt.Errorf("failed to create gateway stream: %w", err)
		}
	default:
		return fmt.Errorf("unsupported stream type: %s", req.Type)
	}
	return nil
}

func (s *ConnectionService) CloseStream(ctx context.Context, streamID string) error {
	return s.connClient.CloseStream(ctx, streamID)
}
