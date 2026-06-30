package conn_client

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
	"github.com/hashicorp/yamux"
)

type LiveConnection struct {
	ConnID      string
	Conn        net.Conn
	Multiplexer *yamux.Session
}

type LiveStream struct {
	StreamID     string
	Type         types.StreamType
	ConnectionID string
	Stream       net.Conn
}

type LiveStreams map[string]*LiveStream

type StreamState struct {
	StreamID     string
	Type         types.StreamType
	ConnectionID string
	State        string // e.g., "active", "inactive", "error"
}

// connWorkerHandshake sends the connection/stream ID to the worker to identify the session.
func connWorkerHandshake(ctx context.Context, conn net.Conn, ID string) error {
	logger := utils.LoggerFromContext(ctx).With("id", ID)
	_, err := conn.Write([]byte(fmt.Sprintf("%s\n", ID)))
	if err != nil {
		logger.Error("failed to send handshake message", "error", err)
		return err
	}
	logger.Info("handshake message sent successfully")
	return nil
}

// spliceStreams copies data bidirectionally between two net.Conn streams.
// cleanupCallback is invoked exactly once when either direction closes.
func spliceStreams(ctx context.Context, stream1, stream2 net.Conn, cleanupCallback func()) {
	logger := utils.LoggerFromContext(ctx)
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			stream1.Close()
			stream2.Close()
			cleanupCallback()
		})
	}

	go func() {
		defer cleanup()
		if _, err := io.Copy(stream1, stream2); err != nil {
			logger.Debug("stream copy ended", "direction", "2->1", "error", err)
		}
	}()

	go func() {
		defer cleanup()
		if _, err := io.Copy(stream2, stream1); err != nil {
			logger.Debug("stream copy ended", "direction", "1->2", "error", err)
		}
	}()
}

// cleanUpStream removes the stream from the active map. Safe to call concurrently.
func (cc *ConnClient) cleanUpStream(ctx context.Context, streamID string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.cleanUpStreamLocked(ctx, streamID)
}

// cleanUpStreamLocked removes a stream entry. Caller must hold cc.mu.
func (cc *ConnClient) cleanUpStreamLocked(ctx context.Context, streamID string) {
	logger := utils.LoggerFromContext(ctx).With("stream_id", streamID)
	if _, exists := cc.streams[streamID]; exists {
		delete(cc.streams, streamID)
		logger.Info("stream removed from active streams")
	}
}

// cleanupConnectionLocked closes all streams and the active connection. Caller must hold cc.mu.
func (cc *ConnClient) cleanupConnectionLocked(ctx context.Context) {
	if cc.connection == nil {
		return
	}
	logger := utils.LoggerFromContext(ctx).With("connection_id", cc.connection.ConnID)
	for streamID := range cc.streams {
		cc.cleanUpStreamLocked(ctx, streamID)
	}
	cc.closeConnectionLocked(ctx)
	logger.Info("connection and all associated streams cleaned up")
}

func (cc *ConnClient) handleConnectionError(ctx context.Context, err error, msg string) {
	logger := utils.LoggerFromContext(ctx)
	if cc.connection != nil {
		logger = logger.With("connection_id", cc.connection.ConnID)
	}
	logger.Error("connection error occurred", "error", err, "message", msg)
	cc.mu.Lock()
	cc.cleanupConnectionLocked(ctx)
	cc.mu.Unlock()
}

func (cc *ConnClient) aliveWatcher() {
	for {
		_, ok := <-cc.aliveChan
		if !ok {
			// Cancelling cc.ctx stops the reconnect loop, which handles cleanup via its ctx.Done() branch.
			cc.cancel()
			return
		}
	}
}

// connect is the reconnect loop: it calls establish and re-dials with exponential backoff whenever the yamux session closes.
func (cc *ConnClient) connect(ctx context.Context, connectionID string) {
	logger := utils.LoggerFromContext(ctx).With("connection_id", connectionID)
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		liveConn, err := cc.establish(ctx, connectionID)
		if err != nil {
			logger.Warn("connection attempt failed, will retry", "error", err, "backoff", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < maxBackoff {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second // reset on success

		// Block until the yamux session closes or context is cancelled.
		select {
		case <-ctx.Done():
			cc.mu.Lock()
			cc.cleanupConnectionLocked(ctx)
			cc.mu.Unlock()
			return
		case <-liveConn.Multiplexer.CloseChan():
			logger.Info("yamux session closed, will reconnect")
			cc.mu.Lock()
			cc.cleanupConnectionLocked(ctx)
			cc.mu.Unlock()
		}
	}
}

// establish makes a single attempt to dial the connection worker and register the connection.
func (cc *ConnClient) establish(ctx context.Context, connectionID string) (*LiveConnection, error) {
	logger := utils.LoggerFromContext(ctx).With("connection_id", connectionID)

	addr := net.JoinHostPort(
		cc.config.ConnectionsClient.Host,
		strconv.Itoa(int(cc.config.ConnectionsClient.Port)),
	)

	tlsConfig, err := clients.GetTLSConfig(cc.config)
	if err != nil {
		return nil, fmt.Errorf("get TLS config: %w", err)
	}

	rawConn, err := (&tls.Dialer{Config: tlsConfig}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("TLS dial %s: %w", addr, err)
	}

	yam, err := yamux.Client(rawConn, nil)
	if err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("yamux client: %w", err)
	}

	if err := connWorkerHandshake(ctx, rawConn, connectionID); err != nil {
		yam.Close()
		return nil, fmt.Errorf("handshake: %w", err)
	}

	liveConn := &LiveConnection{
		ConnID:      connectionID,
		Conn:        rawConn,
		Multiplexer: yam,
	}

	cc.mu.Lock()
	cc.connection = liveConn
	cc.mu.Unlock()

	logger.Info("connection established successfully")
	return liveConn, nil
}

// closeConnectionLocked closes the active connection. Caller must hold cc.mu.
func (cc *ConnClient) closeConnectionLocked(ctx context.Context) error {
	if cc.connection == nil {
		return nil
	}
	logger := utils.LoggerFromContext(ctx).With("connection_id", cc.connection.ConnID)

	if err := cc.connection.Multiplexer.Close(); err != nil {
		logger.Error("failed to close yamux multiplexer", "error", err)
	}
	if err := cc.connection.Conn.Close(); err != nil {
		logger.Error("failed to close underlying connection", "error", err)
	}

	cc.connection = nil
	logger.Info("connection closed")
	return nil
}

// closeStreamLocked closes a specific stream. Caller must hold cc.mu.
func (cc *ConnClient) closeStreamLocked(ctx context.Context, streamID string) error {
	stream, exists := cc.streams[streamID]
	if !exists {
		return fmt.Errorf("stream %s does not exist", streamID)
	}

	if err := stream.Stream.Close(); err != nil {
		utils.LoggerFromContext(ctx).Error("failed to close stream", "stream_id", streamID, "error", err)
		return err
	}

	delete(cc.streams, streamID)
	utils.LoggerFromContext(ctx).Info("stream closed and removed from active streams", "stream_id", streamID)
	return nil
}

func isConnectionActive(conn net.Conn) bool {
	// Set a non-blocking or near-instant deadline so the check doesn't hang
	_ = conn.SetReadDeadline(time.Now().Add(time.Millisecond))
	defer func() {
		// Reset the deadline to zero (infinity) so regular reads work normally
		_ = conn.SetReadDeadline(time.Time{})
	}()

	// Read into a zero-length slice
	var buf [0]byte
	_, err := conn.Read(buf[:])

	// If the remote end closed or hit a hard network error, it won't be nil
	if err != nil {
		// A timeout means the connection is alive but just has no data to read
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return true
		}
		// Any other error (like io.EOF) means the connection is dead
		return false
	}

	return true
}

func getStreamState(stream *LiveStream) string {
	if stream == nil {
		return "inactive"
	}
	if stream.Stream == nil {
		return "inactive"
	}
	if !isConnectionActive(stream.Stream) {
		return "inactive"
	}
	return "active"
}
