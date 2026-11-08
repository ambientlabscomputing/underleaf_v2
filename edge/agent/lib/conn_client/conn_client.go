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

	// remoteTarget is where cloud-opened remote-access streams are spliced to
	// (see remote.go). Guarded by mu. Empty refuses them.
	remoteTarget string

	// loop is the reconnect loop SetConnection owns (the "standing"
	// connection of the remote client). Guarded by loopMu, which also
	// serializes SetConnection calls.
	loopMu sync.Mutex
	loop   *connLoop
}

// connLoop is one running reconnect loop and the means to stop it.
type connLoop struct {
	connectionID string
	cancel       context.CancelFunc
	done         chan struct{} // closed when the loop has returned and cleaned up
}

// Option configures a ConnClient.
type Option func(*ConnClient)

// WithRemoteTarget makes the client serve cloud-opened remote-access streams
// (see remote.go) by splicing them to addr, the Orchestrator's remote listener.
// Without it a ConnClient refuses every cloud-opened stream, which is what the
// gateway client must do: the cloud opens remote-access streams only on the
// dedicated remote client's connection, never on a gateway stream's.
func WithRemoteTarget(addr string) Option {
	return func(cc *ConnClient) { cc.remoteTarget = addr }
}

// NewConnClient builds a client for gateway streams. Pass WithRemoteTarget to
// build the dedicated remote-access client instead.
func NewConnClient(cfg utils.Config, aliveChan chan struct{}, opts ...Option) (*ConnClient, error) {
	hc, err := clients.HttpClientWithCert(cfg)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	cc := &ConnClient{
		hc:        hc,
		config:    cfg,
		streams:   make(LiveStreams),
		aliveChan: aliveChan,
		ctx:       ctx,
		cancel:    cancel,
	}
	for _, o := range opts {
		o(cc)
	}
	return cc, nil
}

func (cc *ConnClient) Start() {
	go cc.aliveWatcher()
}

// Connect starts the reconnect loop for the given connection ID, re-dialing automatically on session loss.
// The loop runs on ConnClient's own long-lived context and is not tied to the caller's request context.
func (cc *ConnClient) Connect(_ context.Context, connectionID string) {
	go cc.connect(cc.ctx, connectionID)
}

// SetConnection makes connectionID the one cloud connection this client keeps
// open, re-dialing it whenever the session is lost. It is for a client that
// owns a single standing connection (the remote client) and is not meant to be
// mixed with Connect on the same client.
//
//   - Same id as the current one: nothing happens (returns false); the running
//     loop keeps the connection.
//   - A different id: the old loop is cancelled and fully torn down (its
//     connection closed) before the new one starts, so the two never race over
//     the client's connection (returns true).
func (cc *ConnClient) SetConnection(connectionID string) (changed bool) {
	cc.loopMu.Lock()
	defer cc.loopMu.Unlock()
	if cc.loop != nil {
		if cc.loop.connectionID == connectionID {
			return false
		}
		cc.loop.cancel()
		<-cc.loop.done
		cc.loop = nil
		// The old loop cleans up after itself; this only guarantees that
		// nothing of the old connection can outlive the switch.
		_ = cc.CloseConnection(cc.ctx)
	}
	ctx, cancel := context.WithCancel(cc.ctx)
	l := &connLoop{connectionID: connectionID, cancel: cancel, done: make(chan struct{})}
	cc.loop = l
	go func() {
		defer close(l.done)
		cc.connect(ctx, connectionID)
	}()
	return true
}

// StandingConnectionID returns the id SetConnection last set, or "".
func (cc *ConnClient) StandingConnectionID() string {
	cc.loopMu.Lock()
	defer cc.loopMu.Unlock()
	if cc.loop == nil {
		return ""
	}
	return cc.loop.connectionID
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

	// Authoritative control-plane check, at the point the local port is dialed:
	// loopback is only a trust boundary if nothing forwards to it, so a stream
	// must never reach the Orchestrator/agent APIs, whatever created it.
	if err := utils.CheckStreamPort(localPort); err != nil {
		logger.Warn("refusing to forward stream to control-plane port", "error", err)
		return nil, err
	}

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
