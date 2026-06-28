package service

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/cloud/conn_worker/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
	"github.com/hashicorp/yamux"
)

type LiveConnection struct {
	TunnelID  string
	RawConn   net.Conn
	YamuxSess *yamux.Session
}

type LiveStream struct {
	StreamID   string
	StreamConn net.Conn
}

type ConnectionManager struct {
	LiveTunnels map[string]*LiveConnection
	LiveStreams map[string]*LiveStream
	listener    net.Listener
	repository  *repository.Repository
	gwChan      chan StreamHandlerReq
	config      utils.Config
}

func NewConnectionManager(
	repository *repository.Repository,
	gwChan chan StreamHandlerReq,
	config utils.Config,
) (*ConnectionManager, error) {
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(config.Connections.NodeConnectionsPort))
	if err != nil {
		utils.Logger.Error("failed to start connection manager", "error", err)
		return nil, err
	}

	return &ConnectionManager{
		LiveTunnels: make(map[string]*LiveConnection),
		LiveStreams: make(map[string]*LiveStream),
		listener:    ln,
		repository:  repository,
		gwChan:      gwChan,
		config:      config,
	}, nil
}

func (cm *ConnectionManager) Serve(ctx context.Context) {
	for {
		conn, err := cm.listener.Accept()
		if err != nil {
			utils.Logger.ErrorContext(ctx, "failed to accept connection", "error", err)
			continue
		}

		go cm.handleNewConnection(ctx, conn)
	}
}

func (cm *ConnectionManager) handleNewConnection(ctx context.Context, conn net.Conn) {
	req, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		utils.Logger.ErrorContext(ctx, "failed to read request", "error", err)
		conn.Close()
		return
	}
	connID := parseIDFromHost(req.Host, ".connect."+utils.GetConfig(utils.ConnWorkerConfig).Connections.Domain)

	// search for the connection_str in the repository
	connection_str, err := cm.repository.Get(ctx, connID)
	if err != nil {
		utils.Logger.ErrorContext(ctx, "failed to get connection from repository", "error", err)
		conn.Close()
		return
	}
	// ensure the connection is not already in LiveTunnels
	if _, exists := cm.LiveTunnels[connID]; exists {
		utils.Logger.ErrorContext(ctx, "connection already exists in LiveTunnels", "conn_id", connID)
		conn.Close()
		return
	}

	var connectionRecord types.Connection
	if err := connection_str.Parse(&connectionRecord); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to parse connection JSON", "error", err)
		conn.Close()
		return
	}

	// ensure the connection is active
	if !isConnectionActive(connectionRecord) {
		utils.Logger.ErrorContext(ctx, "connection is not active", "conn_id", connID)
		conn.Close()
		return
	}

	// Create a new Yamux session for this connection
	yamuxSess, err := yamux.Server(conn, nil)
	if err != nil {
		utils.Logger.ErrorContext(ctx, "failed to create yamux session", "error", err)
		conn.Close()
		return
	}

	cm.LiveTunnels[connID] = &LiveConnection{
		TunnelID:  connID,
		RawConn:   conn,
		YamuxSess: yamuxSess,
	}

	// Handle the Yamux session (e.g., accept streams)
	defer yamuxSess.Close()
	connectionRecord.State = types.ConnectionStateRunning
	connectionRecord.Status = types.StatusSucceeded
	cm.repository.Set(ctx, connectionRecord.ID, string(connectionRecord.ToJSON()), 0) // Mark the connection as active in the repository

	for {
		stream, err := yamuxSess.Accept()
		if err != nil {
			utils.Logger.ErrorContext(ctx, "failed to accept yamux stream", "error", err)
			return
		}

		go cm.HandleStream(ctx, stream)
	}
}

func (cm *ConnectionManager) HandleStream(ctx context.Context, streamConn net.Conn) {
	// record keeping
	// streamID is the first message sent over the stream, terminated by a newline
	reader := bufio.NewReader(streamConn)
	streamID, err := reader.ReadString('\n')
	if err != nil {
		utils.Logger.ErrorContext(ctx, "failed to read stream ID", "error", err)
		streamConn.Close()
		return
	}
	streamID = streamID[:len(streamID)-1] // remove the newline character
	streamStr, err := cm.repository.Get(ctx, streamID)
	if err != nil {
		utils.Logger.ErrorContext(ctx, "failed to get stream from repository", "error", err)
		streamConn.Close()
		return
	}
	cm.LiveStreams[streamID] = &LiveStream{
		StreamID:   streamID,
		StreamConn: streamConn,
	}

	var stream types.Stream
	if err := streamStr.Parse(&stream); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to parse stream JSON", "error", err)
		streamConn.Close()
		return
	}

	if stream.State != types.StreamStateActive {
		utils.Logger.ErrorContext(ctx, "stream is not active", "stream_id", streamID)
		streamConn.Close()
		return
	}

	switch stream.Type {
	case types.StreamTypeGW:
		cm.gwChan <- StreamHandlerReq{
			StreamID: streamID,
			Stream:   streamConn,
			Ctx:      ctx,
		}
	default:
		utils.Logger.ErrorContext(ctx, "unsupported stream type", "stream_id", streamID, "type", stream.Type)
		streamConn.Close()
		return
	}
}

func (cm *ConnectionManager) CloseConnection(ctx context.Context, connID string) error {
	liveConn, exists := cm.LiveTunnels[connID]
	if !exists {
		utils.Logger.ErrorContext(ctx, "connection not found in LiveTunnels", "conn_id", connID)
		return nil
	}

	liveConn.YamuxSess.Close()
	liveConn.RawConn.Close()
	delete(cm.LiveTunnels, connID)

	connection_str, err := cm.repository.Get(ctx, connID)
	if err != nil {
		utils.Logger.ErrorContext(ctx, "failed to get connection from repository", "error", err)
		return err
	}
	if connection_str == "" {
		utils.Logger.ErrorContext(ctx, "connection not found in repository", "conn_id", connID)
		return nil
	}

	var connectionRecord types.Connection
	if err := connection_str.Parse(&connectionRecord); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to parse connection JSON", "error", err)
		return err
	}

	connectionRecord.State = types.ConnectionStateClosed
	connectionRecord.Status = types.StatusSucceeded
	cm.repository.Set(ctx, connectionRecord.ID, string(connectionRecord.ToJSON()), time.Duration(cm.config.Redis.TTLSeconds)*time.Second) // Mark the connection as closed in the repository

	return nil
}

// CloseStream closes a specific stream associated with a connection while leaving the connection itself open. It updates the stream's state in the repository to "closed" and removes it from any active management structures.
func (cm *ConnectionManager) CloseStream(ctx context.Context, streamID string) error {
	streamStr, err := cm.repository.Get(ctx, streamID)
	if err != nil {
		utils.Logger.ErrorContext(ctx, "failed to get stream from repository", "error", err)
		return err
	}
	if streamStr == "" {
		utils.Logger.ErrorContext(ctx, "stream not found in repository", "stream_id", streamID)
		return nil
	}

	var stream types.Stream
	if err := streamStr.Parse(&stream); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to parse stream JSON", "error", err)
		return err
	}

	if liveStream, exists := cm.LiveStreams[streamID]; exists {
		liveStream.StreamConn.Close()
		delete(cm.LiveStreams, streamID)
	}

	stream.State = types.StreamStateClosed
	if err := cm.repository.Set(ctx, stream.ID, string(stream.ToJSON()), time.Duration(cm.config.Redis.TTLSeconds)*time.Second); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to update stream in repository", "error", err)
		return err
	}

	return nil
}

func isConnectionActive(connection types.Connection) bool {
	return connection.State == types.ConnectionStateRunning
}
