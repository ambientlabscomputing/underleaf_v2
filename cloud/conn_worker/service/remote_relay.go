package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/ambientlabscomputing/underleaf_v2/shared/assertion"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// The remote relay (RFD 5, step 7b, cloud half).
//
// A remote human (Cockpit, `ufctl connect`) reaches a cluster like this:
//
//	browser -> nginx -> auth_request to cloud_api (who is this, do they own the
//	cluster, mint a fresh assertion) -> this relay -> yamux stream on the
//	cluster's standing tunnel -> agent -> Orchestrator remote listener
//
// nginx does the authenticating and sets two request headers before proxying
// here:
//
//	X-Underleaf-Assertion  a fresh cloud-signed assertion for this one request
//	X-Underleaf-Connection the id of the cluster's standing cloud connection
//
// The relay is a router and a correct streaming reverse proxy, nothing more.
// It never validates, parses or generates an assertion: the Orchestrator does
// that, and fails closed on a request without a valid one. It also trusts
// nothing the request says except which tunnel to use, and it forwards only a
// scrubbed header set (see scrubRelayHeaders), so a client-supplied
// Authorization / X-Subject-Id / X-Forwarded-* / X-Underleaf-* header cannot
// ride along to the Orchestrator.
//
// The listener is plain HTTP and unauthenticated: anyone who can reach it can
// choose a tunnel and send it requests (which the Orchestrator then refuses
// unless they carry a valid assertion). It must be reachable by nginx only:
// never publish it (no ports: mapping), never put it behind a public route.

const (
	// relayConnectionHeader names the cluster's cloud connection to route to.
	relayConnectionHeader = "X-Underleaf-Connection"
	// relayUpstreamHost is the stable Host/URL host on forwarded requests. The
	// Orchestrator's remote listener does not enforce Host, and the browser's
	// Host is the cloud's, which means nothing there.
	relayUpstreamHost = "127.0.0.1"

	relayMaxHeaderBytes = 16 << 10
)

// Tunables (variables only so tests can shorten them).
var (
	// relayOpenTimeout bounds opening a yamux stream and writing the preamble.
	relayOpenTimeout = 5 * time.Second
	// relayResponseHeaderTimeout bounds the wait for the response header once
	// the request has been written. It does not limit the body, so streamed
	// responses (logs) and upgraded connections (WebSocket) are unaffected.
	relayResponseHeaderTimeout = 30 * time.Second
	// relayMaxStreamsPerConn caps concurrent relayed requests per cluster,
	// below the agent's own cap (128) so one cluster's remote users cannot
	// starve its other cloud-opened streams.
	relayMaxStreamsPerConn = 64
)

var (
	errNotConnected = errors.New("cluster is not connected")
	errStreamOpen   = errors.New("could not open a stream to the cluster")
)

type relayConnKey struct{}

// tunnelLookup finds the live yamux session for a connection id.
// *ConnectionManager implements it.
type tunnelLookup interface {
	Tunnel(id string) (*yamux.Session, bool)
}

// RemoteRelay is the internal HTTP listener that relays remote-access requests
// onto cluster tunnels.
type RemoteRelay struct {
	tunnels tunnelLookup
	addr    string
	proxy   *httputil.ReverseProxy
	limit   *streamLimiter

	mu  sync.Mutex
	srv *http.Server
	ln  net.Listener
}

// NewRemoteRelay returns a relay that will listen on ":<port>".
func NewRemoteRelay(tunnels tunnelLookup, port utils.NamedPort) *RemoteRelay {
	return newRemoteRelay(tunnels, ":"+port.String())
}

func newRemoteRelay(tunnels tunnelLookup, addr string) *RemoteRelay {
	r := &RemoteRelay{
		tunnels: tunnels,
		addr:    addr,
		limit:   newStreamLimiter(),
	}
	r.proxy = &httputil.ReverseProxy{
		Rewrite: rewriteForCluster,
		Transport: &http.Transport{
			DialContext: r.dial,
			// One stream per request: a stream is cheap, and it means a
			// request is never replayed on a stale connection (an assertion's
			// jti is single-use).
			DisableKeepAlives:     true,
			ResponseHeaderTimeout: relayResponseHeaderTimeout,
			ExpectContinueTimeout: time.Second,
		},
		// Negative: flush after every write, so log streams and event streams
		// reach the browser as they are produced.
		FlushInterval: -1,
		ErrorHandler:  r.handleProxyError,
	}
	return r
}

// Handler is the relay's http.Handler.
func (r *RemoteRelay) Handler() http.Handler { return http.HandlerFunc(r.serveHTTP) }

// Start binds the listener and serves in the background. It returns once the
// port is bound, so a port conflict is an error at startup.
func (r *RemoteRelay) Start() error {
	ln, err := net.Listen("tcp", r.addr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           r.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// No ReadTimeout/WriteTimeout: bodies and responses may be long-lived
		// streams. Idle only applies between requests.
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: relayMaxHeaderBytes,
		ErrorLog:       slog.NewLogLogger(utils.Logger.Handler(), slog.LevelWarn),
	}
	r.mu.Lock()
	r.srv, r.ln = srv, ln
	r.mu.Unlock()
	utils.Logger.Info("remote relay listening (internal only: do not publish)", "addr", ln.Addr().String())
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			utils.Logger.Error("remote relay stopped", "error", err)
		}
	}()
	return nil
}

// Addr is the bound address, or nil before Start.
func (r *RemoteRelay) Addr() net.Addr {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ln == nil {
		return nil
	}
	return r.ln.Addr()
}

// Shutdown stops accepting and gives in-flight requests until ctx is done;
// anything still running after that (long streams, WebSockets) is cut.
func (r *RemoteRelay) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	srv := r.srv
	r.mu.Unlock()
	if srv == nil {
		return nil
	}
	err := srv.Shutdown(ctx)
	if err != nil {
		_ = srv.Close()
	}
	return err
}

func (r *RemoteRelay) serveHTTP(w http.ResponseWriter, req *http.Request) {
	conns := req.Header.Values(relayConnectionHeader)

	// /health is the relay's own liveness probe. Traffic from nginx always
	// carries the assertion header, so a request with neither routing header
	// is a direct probe; anything else named /health belongs to the
	// Orchestrator and is relayed like any other path.
	if req.URL.Path == "/health" && (req.Method == http.MethodGet || req.Method == http.MethodHead) &&
		len(conns) == 0 && len(req.Header.Values(assertion.HeaderName)) == 0 {
		writeRelayJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if req.Method == http.MethodConnect {
		writeRelayJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	// Missing, repeated, malformed and unknown ids all get the same answer, so
	// the relay is no oracle for which clusters exist.
	if len(conns) != 1 {
		writeNotConnected(w)
		return
	}
	id := strings.TrimSpace(conns[0])
	if !validConnID(id) {
		writeNotConnected(w)
		return
	}
	if _, ok := r.tunnels.Tunnel(id); !ok {
		writeNotConnected(w)
		return
	}

	if !r.limit.acquire(id) {
		w.Header().Set("Retry-After", "1")
		writeRelayJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many concurrent requests to this cluster"})
		return
	}
	// By default Go's HTTP/1 server stops serving the request body once the
	// response starts. The Transport still reads the inbound body after it has
	// sent it (to check for extra bytes), and an upstream that answers as soon
	// as it has the body (the usual case) makes that read fail with "invalid
	// Read on closed Body"; the Transport then tears down the stream and the
	// response is cut off mid-body. Full duplex lets request and response
	// overlap, which streaming needs anyway.
	_ = http.NewResponseController(w).EnableFullDuplex()

	// Released when the exchange ends, including by the panic ReverseProxy
	// raises (http.ErrAbortHandler) when a response breaks off mid-stream.
	defer r.limit.release(id)

	r.proxy.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), relayConnKey{}, id)))
}

// rewriteForCluster turns the inbound request into the one the Orchestrator's
// remote listener should see. The path and query are forwarded exactly as they
// arrived (nginx has already stripped /clusters/{id}/remote). No assertion is
// ever added: only the one nginx set passes through.
func rewriteForCluster(pr *httputil.ProxyRequest) {
	out := pr.Out
	out.URL.Scheme = "http"
	out.URL.Host = relayUpstreamHost
	out.Host = relayUpstreamHost
	scrubRelayHeaders(out.Header)
}

// scrubRelayHeaders removes everything that could be mistaken for a
// credential or an identity downstream, or that only exists for routing here.
// What is left is the end client's own request headers (Cookie, Origin,
// Content-Type, Accept, Sec-WebSocket-*, ...) plus the assertion.
func scrubRelayHeaders(h http.Header) {
	keep := strings.ToLower(assertion.HeaderName)
	for k := range h {
		l := strings.ToLower(k)
		switch {
		case l == keep:
		case l == "authorization", l == "proxy-authorization",
			l == "x-subject-id", l == "forwarded", l == "x-real-ip",
			strings.HasPrefix(l, "x-forwarded-"),
			strings.HasPrefix(l, "x-underleaf-"): // includes X-Underleaf-Connection
			delete(h, k)
		}
	}
}

// dial ignores the address (it is a placeholder) and returns a fresh yamux
// stream on the cluster's tunnel, with the preamble already written.
func (r *RemoteRelay) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	id, _ := ctx.Value(relayConnKey{}).(string)
	sess, ok := r.tunnels.Tunnel(id)
	if !ok {
		return nil, errNotConnected
	}
	stream, err := openStream(ctx, sess, relayOpenTimeout)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errStreamOpen, err)
	}
	_ = stream.SetWriteDeadline(time.Now().Add(relayOpenTimeout))
	if _, err := io.WriteString(stream, assertion.StreamPreamble); err != nil {
		stream.Close()
		return nil, fmt.Errorf("%w: %v", errStreamOpen, err)
	}
	_ = stream.SetWriteDeadline(time.Time{})
	return stream, nil
}

// openStream opens a yamux stream, giving up after d or when ctx is done. A
// stream that opens after the caller gave up is closed.
func openStream(ctx context.Context, sess *yamux.Session, d time.Duration) (*yamux.Stream, error) {
	type result struct {
		s   *yamux.Stream
		err error
	}
	ch := make(chan result, 1)
	go func() {
		s, err := sess.OpenStream()
		ch <- result{s, err}
	}()
	timer := time.NewTimer(d)
	defer timer.Stop()
	abandon := func() {
		go func() {
			if r := <-ch; r.s != nil {
				r.s.Close()
			}
		}()
	}
	select {
	case r := <-ch:
		return r.s, r.err
	case <-timer.C:
		abandon()
		return nil, errors.New("timed out opening stream")
	case <-ctx.Done():
		abandon()
		return nil, ctx.Err()
	}
}

func (r *RemoteRelay) handleProxyError(w http.ResponseWriter, req *http.Request, err error) {
	id, _ := req.Context().Value(relayConnKey{}).(string)
	switch {
	case errors.Is(err, context.Canceled):
		// The client went away; there is nobody to answer.
		return
	case errors.Is(err, errNotConnected):
		writeNotConnected(w)
	case errors.Is(err, errStreamOpen):
		utils.Logger.Warn("remote relay: stream open failed", "conn_id", id, "error", err)
		writeRelayJSON(w, http.StatusBadGateway, map[string]string{"error": "cluster did not accept the request"})
	case isTimeout(err):
		utils.Logger.Warn("remote relay: cluster timed out", "conn_id", id, "error", err)
		writeRelayJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "cluster did not respond in time"})
	default:
		// Includes the tunnel dying under an in-flight request (EOF, reset,
		// session shutdown).
		utils.Logger.Warn("remote relay: upstream error", "conn_id", id, "error", err)
		writeRelayJSON(w, http.StatusBadGateway, map[string]string{"error": "bad gateway"})
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
}

func writeNotConnected(w http.ResponseWriter) {
	writeRelayJSON(w, http.StatusServiceUnavailable, map[string]string{"error": errNotConnected.Error()})
}

func writeRelayJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// streamLimiter counts in-flight relayed requests per connection id.
type streamLimiter struct {
	mu sync.Mutex
	n  map[string]int
}

func newStreamLimiter() *streamLimiter { return &streamLimiter{n: make(map[string]int)} }

func (l *streamLimiter) acquire(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.n[id] >= relayMaxStreamsPerConn {
		return false
	}
	l.n[id]++
	return true
}

func (l *streamLimiter) release(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.n[id] <= 1 {
		delete(l.n, id)
		return
	}
	l.n[id]--
}
