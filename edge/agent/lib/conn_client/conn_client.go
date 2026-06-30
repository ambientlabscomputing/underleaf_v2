package conn_client

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// ConnClient provides a low level API for interacting with the connection service. It is used by the agent to manage connections to the cloud.
type ConnClient struct {
	hc         *http.Client
	config     utils.Config
	mu         sync.RWMutex
	connection *LiveConnection // one connection per node agent
	streams    LiveStreams     // streams are multiplexed over the connection
	aliveChan  chan struct{}   // channel to signal that the agent is alive and should keep the connection open
	ctx        context.Context // long-lived context owned by ConnClient, cancelled on shutdown
	cancel     context.CancelFunc
}

func NewConnClient(cfg utils.Config, aliveChan chan struct{}) (*ConnClient, error) {
	hc, err := clients.HttpClientWithCert(cfg)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &ConnClient{
		hc:        hc,
		config:    cfg,
		streams:   make(LiveStreams),
		aliveChan: aliveChan,
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

func (cc *ConnClient) Start() {
	go cc.aliveWatcher()
}

// Connect starts the reconnect loop for the given connection ID, re-dialing automatically on session loss.
// The loop runs on ConnClient's own long-lived context and is not tied to the caller's request context.
func (cc *ConnClient) Connect(_ context.Context, connectionID string) {
	go cc.connect(cc.ctx, connectionID)
}

// CloseConnection tears down the active connection and all its streams.
func (cc *ConnClient) CloseConnection(ctx context.Context) error {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	return cc.closeConnectionLocked(ctx)
}

// CloseStream tears down the given stream and removes it from the active streams map.
func (cc *ConnClient) CloseStream(ctx context.Context, streamID string) error {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	return cc.closeStreamLocked(ctx, streamID)
}

func (cc *ConnClient) GetStream(streamID string) (StreamState, bool) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	stream, exists := cc.streams[streamID]
	if !exists {
		return StreamState{}, false
	}
	return StreamState{
		StreamID:     stream.StreamID,
		Type:         stream.Type,
		ConnectionID: stream.ConnectionID,
		State:        getStreamState(stream),
	}, true
}

func (cc *ConnClient) ListStreams() []StreamState {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	streamStates := make([]StreamState, 0, len(cc.streams))
	for _, stream := range cc.streams {
		streamStates = append(streamStates, StreamState{
			StreamID:     stream.StreamID,
			Type:         stream.Type,
			ConnectionID: stream.ConnectionID,
			State:        getStreamState(stream),
		})
	}
	return streamStates
}

// NewGatewayStream establishes a new stream over the existing connection specifically for gateway communication.
// Dials local port and copies data between the local port and the stream. Returns a LiveStream object that can be used to interact with the stream.
func (cc *ConnClient) NewGatewayStream(ctx context.Context, streamID string, streamType types.StreamType, localPort int) (*LiveStream, error) {
	logger := utils.LoggerFromContext(ctx).With("stream_id", streamID, "stream_type", streamType, "local_port", localPort)

	cc.mu.Lock()
	if cc.connection == nil {
		cc.mu.Unlock()
		return nil, fmt.Errorf("no active connection to create a gateway stream")
	}
	mux := cc.connection.Multiplexer
	connID := cc.connection.ConnID
	cc.mu.Unlock()

	localPortConn, err := (&net.Dialer{}).DialContext(ctx, "tcp", fmt.Sprintf("localhost:%d", localPort))
	if err != nil {
		logger.Error("failed to connect to local port", "error", err)
		return nil, err
	}

	stream, err := mux.Open()
	if err != nil {
		localPortConn.Close()
		logger.Error("failed to open new stream over multiplexer", "error", err)
		return nil, err
	}

	// Handshake before registering the stream; close resources on failure.
	if err := connWorkerHandshake(ctx, stream, streamID); err != nil {
		stream.Close()
		localPortConn.Close()
		logger.Error("failed to perform stream handshake", "error", err)
		return nil, err
	}

	liveStream := &LiveStream{
		StreamID:     streamID,
		Type:         streamType,
		ConnectionID: connID,
		Stream:       stream,
	}

	cc.mu.Lock()
	cc.streams[streamID] = liveStream
	cc.mu.Unlock()

	spliceStreams(ctx, localPortConn, stream, func() {
		cc.cleanUpStream(ctx, streamID)
	})

	logger.Info("gateway stream established successfully")
	return liveStream, nil
}
