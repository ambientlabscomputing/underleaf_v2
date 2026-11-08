package service

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
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
	// Counter is the same object as StreamConn (wrapped once at creation in
	// HandleStream), kept as its concrete type so byte counts can be read
	// back for flushing into Redis without a type assertion at each call site.
	Counter *countingConn
}

// connStore is the slice of the Redis repository the manager uses (so tests
// can run without Redis). *repository.Repository implements it.
type connStore interface {
	Get(ctx context.Context, key string) (repository.RepositoryResult, error)
	Set(ctx context.Context, key string, value interface{}, timeout time.Duration) error
}

type ConnectionManager struct {
	// mu guards LiveTunnels and LiveStreams, which are read and written from
	// the accept loop, one goroutine per tunnel, one per stream, the gRPC
	// handlers, the relay and the periodic byte flusher. Never hold it across
	// I/O. Use the accessor methods; direct map access outside this file's
	// helpers is a data race.
	mu          sync.RWMutex
	LiveTunnels map[string]*LiveConnection
	LiveStreams map[string]*LiveStream
	listener    net.Listener
	repository  connStore
	gwChan      chan StreamHandlerReq
	config      utils.Config
}

func NewConnectionManager(
	repository *repository.Repository,
	gwChan chan StreamHandlerReq,
	config utils.Config,
) (*ConnectionManager, error) {
	ln, err := net.Listen("tcp", ":"+config.Connections.NodeConnectionsPort.String())
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
	go cm.PeriodicallyFlushStreamBytes(ctx)
	for {
		conn, err := cm.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			utils.Logger.ErrorContext(ctx, "failed to accept connection", "error", err)
			continue
		}

		go cm.handleNewConnection(ctx, conn)
	}
}

// Handshake limits. A tunnel dial is mTLS-authenticated by nginx before it gets
// here, but the bytes after that are still untrusted input.
const (
	handshakeTimeout = 10 * time.Second
	// maxHandshakeLine bounds the first line the peer may send (a bare
	// connection id, or an HTTP request line in the legacy form).
	maxHandshakeLine = 2048
	maxConnIDLen     = 128
)

// tunnelProbeTimeout is how long a redial waits for the previous session with
// the same connection id to answer a yamux ping before that session is treated
// as dead (a half-open TCP connection after a network drop looks alive to the
// cloud until keepalives fail, and the agent should not have to wait for that).
var tunnelProbeTimeout = 3 * time.Second

var (
	errTunnelExists  = errors.New("connection already has a live tunnel")
	errHandshakeLine = errors.New("handshake line too long")
	errBadHandshake  = errors.New("not a connection handshake")
)

var connIDPrefix = string(types.ConnectionIDPrefix) + "_"

// validConnID reports whether id is shaped like a connection id
// ("conn_<xid>"): bounded, and only characters that can never matter to Redis,
// log lines or HTTP framing.
func validConnID(id string) bool {
	if len(id) <= len(connIDPrefix) || len(id) > maxConnIDLen || !strings.HasPrefix(id, connIDPrefix) {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// bufferedConn is conn with a reader in front: the handshake is read through a
// bufio.Reader, which may already hold the first yamux frames, and those bytes
// must reach the yamux session instead of being lost with the reader.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// peekLine returns the first line in br (including the newline) without
// consuming it, failing if there is none within max bytes.
func peekLine(br *bufio.Reader, max int) ([]byte, error) {
	for n := 1; n <= max; n++ {
		b, err := br.Peek(n)
		if err != nil {
			return nil, err
		}
		if b[n-1] == '\n' {
			return b, nil
		}
	}
	return nil, errHandshakeLine
}

// readHandshake reads the connection id a dialing agent announces and returns
// it with the reader that now holds any bytes read past the handshake.
//
// The agent (edge/agent/lib/conn_client, connWorkerHandshake) writes
// "<connection_id>\n" on the raw connection and then speaks yamux. nginx's
// stream proxy on :19021 terminates TLS and passes those bytes through, so
// there is no HTTP request and no Host header on this connection. The older
// form this code used to require, an HTTP request whose Host is
// "<connection_id>.connect.<domain>", is still accepted for anything that
// speaks it (nothing in this repository does).
func readHandshake(conn net.Conn, domain string) (string, *bufio.Reader, error) {
	_ = conn.SetReadDeadline(time.Now().Add(handshakeTimeout))
	defer conn.SetReadDeadline(time.Time{})

	br := bufio.NewReaderSize(conn, 4096)
	line, err := peekLine(br, maxHandshakeLine)
	if err != nil {
		return "", nil, err
	}
	text := strings.TrimRight(string(line), "\r\n")

	if validConnID(text) {
		_, _ = br.Discard(len(line))
		return text, br, nil
	}
	if strings.HasSuffix(text, " HTTP/1.1") || strings.HasSuffix(text, " HTTP/1.0") {
		req, err := http.ReadRequest(br)
		if err != nil {
			return "", nil, err
		}
		if id := parseIDFromHost(req.Host, ".connect."+domain); validConnID(id) {
			return id, br, nil
		}
	}
	return "", nil, errBadHandshake
}

func (cm *ConnectionManager) handleNewConnection(ctx context.Context, conn net.Conn) {
	domain := ""
	if cm.config.Connections != nil {
		domain = cm.config.Connections.Domain
	}
	connID, br, err := readHandshake(conn, domain)
	if err != nil {
		utils.Logger.ErrorContext(ctx, "invalid connection handshake", "error", err)
		conn.Close()
		return
	}

	// search for the connection_str in the repository
	connection_str, err := cm.repository.Get(ctx, connID)
	if err != nil {
		utils.Logger.ErrorContext(ctx, "failed to get connection from repository", "error", err)
		conn.Close()
		return
	}

	var connectionRecord types.Connection
	if err := connection_str.Parse(&connectionRecord); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to parse connection JSON", "error", err)
		conn.Close()
		return
	}

	// ensure the connection may be dialed
	if !canEstablish(connectionRecord) {
		utils.Logger.ErrorContext(ctx, "connection cannot be established in its current state", "conn_id", connID, "state", connectionRecord.State)
		conn.Close()
		return
	}

	// Create a new Yamux session for this connection. It reads through br so
	// frames the agent sent right behind the handshake are not lost.
	yamuxSess, err := yamux.Server(&bufferedConn{Conn: conn, r: br}, nil)
	if err != nil {
		utils.Logger.ErrorContext(ctx, "failed to create yamux session", "error", err)
		conn.Close()
		return
	}

	live := &LiveConnection{
		TunnelID:  connID,
		RawConn:   conn,
		YamuxSess: yamuxSess,
	}
	if err := cm.registerTunnel(connID, live); err != nil {
		utils.Logger.ErrorContext(ctx, "refusing tunnel", "conn_id", connID, "error", err)
		yamuxSess.Close()
		conn.Close()
		return
	}
	// However the session ends, drop it from LiveTunnels (only if it is still
	// this one: a redial may already have replaced it) so the agent can redial.
	defer func() {
		cm.removeTunnel(connID, yamuxSess)
		yamuxSess.Close()
		conn.Close()
	}()

	connectionRecord.State = types.ConnectionStateRunning
	connectionRecord.Status = types.StatusSucceeded
	if err := cm.repository.Set(ctx, connectionRecord.ID, string(connectionRecord.ToJSON()), 0); err != nil { // Mark the connection as active in the repository
		utils.Logger.ErrorContext(ctx, "failed to mark connection running", "conn_id", connID, "error", err)
	}

	for {
		stream, err := yamuxSess.Accept()
		if err != nil {
			utils.Logger.InfoContext(ctx, "tunnel session ended", "conn_id", connID, "error", err)
			return
		}

		go cm.HandleStream(ctx, stream)
	}
}

// canEstablish reports whether an agent may open a tunnel for this record: a
// freshly provisioned connection (its first dial), or a running one (the agent
// redialing after a drop). A closed connection stays closed.
//
// This used to demand "running", but nothing ever sets that before the first
// dial (NewConnection stores "provisioned" and only this handler promotes the
// record), so no tunnel could ever be established.
func canEstablish(c types.Connection) bool {
	return c.State == types.ConnectionStateProvisioned || c.State == types.ConnectionStateRunning
}

// Tunnel returns the live, open session for a connection id.
func (cm *ConnectionManager) Tunnel(id string) (*yamux.Session, bool) {
	cm.mu.RLock()
	lc, ok := cm.LiveTunnels[id]
	cm.mu.RUnlock()
	if !ok || lc.YamuxSess == nil || lc.YamuxSess.IsClosed() {
		return nil, false
	}
	return lc.YamuxSess, true
}

// registerTunnel records lc as the tunnel for id. If an older session is still
// registered for id it is replaced only when it is dead: closed, or failing a
// yamux ping. A live tunnel is never displaced by a second dial with the same
// id.
func (cm *ConnectionManager) registerTunnel(id string, lc *LiveConnection) error {
	for attempt := 0; ; attempt++ {
		cm.mu.Lock()
		old, exists := cm.LiveTunnels[id]
		if !exists || old.YamuxSess == nil || old.YamuxSess.IsClosed() {
			cm.LiveTunnels[id] = lc
			cm.mu.Unlock()
			return nil
		}
		cm.mu.Unlock()

		if attempt > 0 || sessionAlive(old.YamuxSess, tunnelProbeTimeout) {
			return errTunnelExists
		}
		// Unresponsive: tear it down, then the next pass replaces it.
		old.YamuxSess.Close()
		if old.RawConn != nil {
			old.RawConn.Close()
		}
	}
}

// removeTunnel deletes id from LiveTunnels only if it still maps to sess.
func (cm *ConnectionManager) removeTunnel(id string, sess *yamux.Session) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if lc, ok := cm.LiveTunnels[id]; ok && lc.YamuxSess == sess {
		delete(cm.LiveTunnels, id)
	}
}

// sessionAlive pings sess and reports whether it answered within d.
func sessionAlive(sess *yamux.Session, d time.Duration) bool {
	done := make(chan bool, 1)
	go func() {
		_, err := sess.Ping()
		done <- err == nil
	}()
	select {
	case ok := <-done:
		return ok
	case <-time.After(d):
		return false
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

	// Wrap once, here, so every consumer of this conn (LiveStreams tracking,
	// the gateway's bridge() via gwChan) transparently gets byte counting --
	// bridge() just calls io.Copy on the net.Conn interface, no changes
	// needed there.
	counted := newCountingConn(streamConn)
	cm.mu.Lock()
	cm.LiveStreams[streamID] = &LiveStream{
		StreamID:   streamID,
		StreamConn: counted,
		Counter:    counted,
	}
	cm.mu.Unlock()

	switch stream.Type {
	case types.StreamTypeGW:
		cm.gwChan <- StreamHandlerReq{
			StreamID: streamID,
			Stream:   counted,
			Ctx:      ctx,
		}
	default:
		utils.Logger.ErrorContext(ctx, "unsupported stream type", "stream_id", streamID, "type", stream.Type)
		cm.mu.Lock()
		delete(cm.LiveStreams, streamID)
		cm.mu.Unlock()
		streamConn.Close()
		return
	}
}

func (cm *ConnectionManager) CloseConnection(ctx context.Context, connID string) error {
	cm.mu.Lock()
	liveConn, exists := cm.LiveTunnels[connID]
	delete(cm.LiveTunnels, connID)
	cm.mu.Unlock()
	if !exists {
		utils.Logger.ErrorContext(ctx, "connection not found in LiveTunnels", "conn_id", connID)
		return nil
	}

	liveConn.YamuxSess.Close()
	if liveConn.RawConn != nil {
		liveConn.RawConn.Close()
	}

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

	cm.mu.Lock()
	liveStream, exists := cm.LiveStreams[streamID]
	delete(cm.LiveStreams, streamID)
	cm.mu.Unlock()
	if exists {
		if liveStream.Counter != nil {
			stream.BytesIn = liveStream.Counter.BytesRead()
			stream.BytesOut = liveStream.Counter.BytesWritten()
		}
		liveStream.StreamConn.Close()
	}

	stream.State = types.StreamStateClosed
	closedAt := time.Now().UTC().Format(time.RFC3339)
	stream.ClosedAt = &closedAt
	// Extend past the configured TTL by closedRecordFlushGrace so
	// cloud_api's poll loop has a real window to read final byte counts
	// even if its own sync cycle is stalled at the moment this stream
	// closes (see track 5 plan §2's Redis-TTL-vs-poll-cadence risk).
	ttl := time.Duration(cm.config.Redis.TTLSeconds)*time.Second + closedRecordFlushGrace
	if err := cm.repository.Set(ctx, stream.ID, string(stream.ToJSON()), ttl); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to update stream in repository", "error", err)
		return err
	}

	return nil
}

// PeriodicallyFlushStreamBytes persists still-active streams' running byte
// counts into Redis every flushIntervalSeconds, so a crash mid-stream loses
// at most one flush interval's worth of usage instead of everything back to
// stream creation. Intended to be run as its own goroutine for the lifetime
// of the process (see Serve()).
func (cm *ConnectionManager) PeriodicallyFlushStreamBytes(ctx context.Context) {
	ticker := time.NewTicker(flushIntervalSeconds * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cm.flushActiveStreamBytes(ctx)
		}
	}
}

func (cm *ConnectionManager) flushActiveStreamBytes(ctx context.Context) {
	cm.mu.RLock()
	live := make(map[string]*LiveStream, len(cm.LiveStreams))
	for id, ls := range cm.LiveStreams {
		live[id] = ls
	}
	cm.mu.RUnlock()
	for streamID, liveStream := range live {
		if liveStream.Counter == nil {
			continue
		}
		streamStr, err := cm.repository.Get(ctx, streamID)
		if err != nil || streamStr == "" {
			continue
		}
		var stream types.Stream
		if err := streamStr.Parse(&stream); err != nil {
			utils.Logger.ErrorContext(ctx, "failed to unmarshal stream during byte flush", "error", err, "stream_id", streamID)
			continue
		}
		stream.BytesIn = liveStream.Counter.BytesRead()
		stream.BytesOut = liveStream.Counter.BytesWritten()
		// 0 TTL: same "no expiry while active" convention NewStream uses.
		if err := cm.repository.Set(ctx, stream.ID, string(stream.ToJSON()), 0); err != nil {
			utils.Logger.ErrorContext(ctx, "failed to flush stream bytes", "error", err, "stream_id", streamID)
		}
	}
}
