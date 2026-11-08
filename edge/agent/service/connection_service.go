package service

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/lib/conn_client"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// StandingConnection is the dedicated remote-access client: it holds one cloud
// connection open and serves the streams the cloud opens on it. *ConnClient
// implements it; tests use a fake.
type StandingConnection interface {
	Start()
	// SetConnection (re)starts the reconnect loop for id; false means id was
	// already the standing connection.
	SetConnection(id string) (changed bool)
}

// ConnectionService is the agent's entrypoint for interacting with the cloud connection service.
//
// It owns two clients that never share a connection: connClient carries the
// gateway streams the agent opens, remote carries the cluster's standing
// remote-access connection (see SetRemoteConnection).
type ConnectionService struct {
	connClient *conn_client.ConnClient
	config     utils.Config

	remote       StandingConnection // nil: remote access is not available
	remoteTarget string             // UNDERLEAF_REMOTE_TARGET; "" refuses SetRemoteConnection
	// overrideID, when set (UNDERLEAF_REMOTE_CONNECTION_ID), pins the standing
	// connection: commands from the Orchestrator do not move it.
	overrideID string
}

func NewConnectionService(config utils.Config, cc *conn_client.ConnClient) *ConnectionService {
	return &ConnectionService{
		connClient: cc,
		config:     config,
	}
}

// WithRemote attaches the dedicated remote-access client. target is where it
// splices cloud-opened streams (the client must have been built with the same
// conn_client.WithRemoteTarget); override is the optional manual connection id
// (UNDERLEAF_REMOTE_CONNECTION_ID).
func (s *ConnectionService) WithRemote(remote StandingConnection, target, override string) *ConnectionService {
	s.remote = remote
	s.remoteTarget = strings.TrimSpace(target)
	s.overrideID = strings.TrimSpace(override)
	return s
}

func (s *ConnectionService) Start() {
	if s.connClient != nil {
		s.connClient.Start()
	}
	if s.remote == nil {
		return
	}
	s.remote.Start()
	// Manual override: pin the standing connection to a configured id. Without
	// it, the Orchestrator tells the agent which connection to hold
	// (SetRemoteConnection). Needs the target, else the connection would be
	// open for nothing.
	if s.overrideID != "" && s.remoteTarget != "" {
		s.remote.SetConnection(s.overrideID)
	}
}

// SetRemoteConnection makes connectionID the cluster's standing remote-access
// connection: the remote client keeps it open (re-dialing on loss) so the cloud
// can open remote-access streams toward this agent at any time. Idempotent for
// the same id; a new id switches cleanly (the old connection is closed first).
func (s *ConnectionService) SetRemoteConnection(connectionID string) error {
	connectionID = strings.TrimSpace(connectionID)
	if connectionID == "" {
		return status.Error(codes.InvalidArgument, "a remote connection needs a connection id")
	}
	if s.remote == nil || s.remoteTarget == "" {
		return status.Error(codes.FailedPrecondition, "remote access is not configured on this agent ("+utils.EnvRemoteTarget+" is not set)")
	}
	if s.overrideID != "" {
		utils.Logger.Info("remote access: ignoring the Orchestrator's connection; the connection id is pinned by "+utils.EnvRemoteConnectionID,
			"offered", connectionID, "pinned", s.overrideID)
		return nil
	}
	if s.remote.SetConnection(connectionID) {
		utils.Logger.Info("remote access: standing connection set", "connection_id", connectionID)
	}
	return nil
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
	// Fail fast, before dialing the cloud. NewGatewayStream enforces the same
	// check again where the local port is actually dialed.
	if err := utils.CheckStreamPort(req.Port); err != nil {
		return status.Error(codes.PermissionDenied, err.Error())
	}
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
