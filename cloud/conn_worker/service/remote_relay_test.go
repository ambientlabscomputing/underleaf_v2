package service

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/lib/conn_client"
	"github.com/ambientlabscomputing/underleaf_v2/shared/assertion"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// cluster is a fake cluster: a fake Orchestrator remote listener, behind the
// production agent handler (conn_client.ServeRemoteStreams) on one end of an
// in-process yamux pair whose other end is registered with the manager.
type cluster struct {
	id       string
	upstream *httptest.Server
	cloud    *yamux.Session
	agent    *yamux.Session
}

func addCluster(t *testing.T, cm *ConnectionManager, id string, h http.Handler) *cluster {
	t.Helper()
	up := httptest.NewServer(h)
	t.Cleanup(up.Close)
	cloud, agent := yamuxPair(t)
	go conn_client.ServeRemoteStreams(context.Background(), agent, func() string { return up.Listener.Addr().String() })
	if err := cm.registerTunnel(id, &LiveConnection{TunnelID: id, YamuxSess: cloud}); err != nil {
		t.Fatal(err)
	}
	return &cluster{id: id, upstream: up, cloud: cloud, agent: agent}
}

func newRelayManager() *ConnectionManager {
	return &ConnectionManager{LiveTunnels: map[string]*LiveConnection{}, LiveStreams: map[string]*LiveStream{}}
}

func startRelay(t *testing.T, cm *ConnectionManager) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(newRemoteRelay(cm, "").Handler())
	t.Cleanup(srv.Close)
	return srv
}

func relayGet(t *testing.T, url, connID, path string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest("GET", url+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if connID != "" {
		req.Header.Set(relayConnectionHeader, connID)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestRelayRoutesByConnectionHeader(t *testing.T) {
	cm := newRelayManager()
	for _, name := range []string{"conn_a", "conn_b"} {
		addCluster(t, cm, name, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "I am "+name)
		}))
	}
	relay := startRelay(t, cm)
	for _, name := range []string{"conn_a", "conn_b", "conn_a"} {
		if _, body := relayGet(t, relay.URL, name, "/x", nil); body != "I am "+name {
			t.Fatalf("%s routed to %q", name, body)
		}
	}
}

func TestRelayNotConnectedIsUniform(t *testing.T) {
	cm := newRelayManager()
	dead := addCluster(t, cm, "conn_dead", http.NotFoundHandler())
	addCluster(t, cm, "conn_ok", http.NotFoundHandler())
	dead.cloud.Close() // registered, but the session is gone
	relay := startRelay(t, cm)

	type probe struct {
		name string
		hdr  []string // raw values of X-Underleaf-Connection; nil = absent
	}
	probes := []probe{
		{"absent", nil},
		{"empty", []string{""}},
		{"unknown", []string{"conn_nobody"}},
		{"malformed", []string{"conn_a b/../x"}},
		{"not a connection id", []string{"stream_abc"}},
		{"dead session", []string{"conn_dead"}},
		{"repeated", []string{"conn_ok", "conn_ok"}},
		{"very long", []string{"conn_" + strings.Repeat("a", 5000)}},
	}
	var wantBody string
	for i, p := range probes {
		req, _ := http.NewRequest("GET", relay.URL+"/api/v1/x", nil)
		for _, v := range p.hdr {
			req.Header.Add(relayConnectionHeader, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 503 {
			t.Errorf("%s: status %d", p.name, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s: content-type %q", p.name, ct)
		}
		if i == 0 {
			wantBody = string(b)
			if strings.TrimSpace(wantBody) != `{"error":"cluster is not connected"}` {
				t.Fatalf("body = %q", wantBody)
			}
		} else if string(b) != wantBody {
			t.Errorf("%s: body %q differs from %q", p.name, b, wantBody)
		}
	}
}

// Each scenario below drives the production agent handler, which itself
// refuses a stream that does not begin with assertion.StreamPreamble, so a
// successful response proves the preamble came first.
func TestRelayForwardsScrubbedRequest(t *testing.T) {
	type seenReq struct {
		method, uri, host, body string
		hdr                     http.Header
	}
	got := make(chan seenReq, 1)
	cm := newRelayManager()
	addCluster(t, cm, "conn_h", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- seenReq{r.Method, r.RequestURI, r.Host, string(b), r.Header.Clone()}
		w.Header().Set("X-Orch", "yes")
		w.WriteHeader(201)
		fmt.Fprint(w, "created")
	}))
	relay := startRelay(t, cm)

	req, _ := http.NewRequest("POST", relay.URL+"/api/v1/streams/a%2Fb?x=1&y=%20z", strings.NewReader(`{"k":1}`))
	for k, v := range map[string]string{
		relayConnectionHeader:         "conn_h",
		assertion.HeaderName:          "header.payload.signature",
		"Content-Type":                "application/json",
		"Cookie":                      "ui=1",
		"Origin":                      "https://cockpit.example",
		"Authorization":               "Bearer local-token",
		"X-Subject-Id":                "someone-else",
		"X-Forwarded-For":             "6.6.6.6",
		"X-Forwarded-Host":            "evil.example",
		"X-Forwarded-Proto":           "https",
		"X-Forwarded-Port":            "443",
		"X-Real-Ip":                   "6.6.6.6",
		"Forwarded":                   "for=6.6.6.6",
		"X-Underleaf-Foo":             "bar",
		"X-Underleaf-Assertion-Extra": "x",
		"Proxy-Authorization":         "Basic abc",
	} {
		req.Header.Set(k, v)
	}
	req.Host = "cockpit.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 201 || string(b) != "created" || resp.Header.Get("X-Orch") != "yes" {
		t.Fatalf("response = %d %q %v", resp.StatusCode, b, resp.Header)
	}

	s := <-got
	if s.method != "POST" || s.uri != "/api/v1/streams/a%2Fb?x=1&y=%20z" || s.body != `{"k":1}` {
		t.Fatalf("request line/body altered: %+v", s)
	}
	if s.host != "127.0.0.1" {
		t.Fatalf("Host = %q", s.host)
	}
	if v := s.hdr.Values(assertion.HeaderName); len(v) != 1 || v[0] != "header.payload.signature" {
		t.Fatalf("assertion = %v", v)
	}
	for _, h := range []string{"Authorization", "Proxy-Authorization", "X-Subject-Id", "X-Forwarded-For", "X-Forwarded-Host",
		"X-Forwarded-Proto", "X-Forwarded-Port", "X-Real-Ip", "Forwarded", "X-Underleaf-Foo", "X-Underleaf-Assertion-Extra", relayConnectionHeader} {
		if v := s.hdr.Values(h); len(v) != 0 {
			t.Errorf("%s leaked to the cluster: %v", h, v)
		}
	}
	if s.hdr.Get("Cookie") != "ui=1" || s.hdr.Get("Origin") != "https://cockpit.example" || s.hdr.Get("Content-Type") != "application/json" {
		t.Errorf("ordinary headers were not forwarded: %v", s.hdr)
	}
}

func TestRelayAddsNoAssertionOfItsOwn(t *testing.T) {
	got := make(chan []string, 1)
	cm := newRelayManager()
	addCluster(t, cm, "conn_n", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Values(assertion.HeaderName)
	}))
	relay := startRelay(t, cm)
	relayGet(t, relay.URL, "conn_n", "/", nil)
	if v := <-got; len(v) != 0 {
		t.Fatalf("relay invented an assertion: %v", v)
	}
}

// The relay itself must put the preamble first on the stream, ahead of the
// HTTP request (checked against a hand-rolled agent, not the production one).
func TestRelayWritesPreambleBeforeTheRequest(t *testing.T) {
	cm := newRelayManager()
	cloud, agent := yamuxPair(t)
	if err := cm.registerTunnel("conn_p", &LiveConnection{YamuxSess: cloud}); err != nil {
		t.Fatal(err)
	}
	first := make(chan string, 1)
	go func() {
		s, err := agent.AcceptStream()
		if err != nil {
			return
		}
		br := bufio.NewReader(s)
		pre := make([]byte, len(assertion.StreamPreamble))
		io.ReadFull(br, pre)
		line, _ := br.ReadString('\n')
		first <- string(pre) + "|" + strings.TrimSpace(line)
		s.Write([]byte("HTTP/1.1 204 No Content\r\n\r\n"))
		s.Close()
	}()
	relay := startRelay(t, cm)
	relayGet(t, relay.URL, "conn_p", "/hello", nil)
	if got, want := <-first, assertion.StreamPreamble+"|GET /hello HTTP/1.1"; got != want {
		t.Fatalf("stream began %q, want %q", got, want)
	}
}

func TestRelayWebSocketRoundTrip(t *testing.T) {
	var upHdr atomic.Value
	up := websocket.Upgrader{}
	cm := newRelayManager()
	addCluster(t, cm, "conn_ws", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upHdr.Store(r.Header.Clone())
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		c.WriteMessage(websocket.TextMessage, []byte("welcome")) // server speaks first
		for {
			mt, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			c.WriteMessage(mt, append([]byte("echo:"), msg...))
		}
	}))
	relay := startRelay(t, cm)

	hdr := http.Header{}
	hdr.Set(relayConnectionHeader, "conn_ws")
	hdr.Set(assertion.HeaderName, "ws.assertion")
	hdr.Set("Authorization", "Bearer nope")
	c, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(relay.URL, "http")+"/api/v1/ws", hdr)
	if err != nil {
		t.Fatalf("dial: %v (resp %v)", err, resp)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, m, err := c.ReadMessage(); err != nil || string(m) != "welcome" {
		t.Fatalf("first message = %q, %v", m, err)
	}
	for i := 0; i < 3; i++ {
		msg := fmt.Sprintf("ping-%d", i)
		if err := c.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
			t.Fatal(err)
		}
		if _, m, err := c.ReadMessage(); err != nil || string(m) != "echo:"+msg {
			t.Fatalf("echo = %q, %v", m, err)
		}
	}
	h := upHdr.Load().(http.Header)
	if h.Get(assertion.HeaderName) != "ws.assertion" || h.Get("Authorization") != "" || h.Get(relayConnectionHeader) != "" {
		t.Fatalf("upgrade request headers = %v", h)
	}
}

func TestRelayStreamsResponsesIncrementally(t *testing.T) {
	release := make(chan struct{})
	cm := newRelayManager()
	addCluster(t, cm, "conn_s", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: one\n\n")
		w.(http.Flusher).Flush()
		<-release
		fmt.Fprint(w, "data: two\n\n")
	}))
	relay := startRelay(t, cm)

	req, _ := http.NewRequest("GET", relay.URL+"/logs", nil)
	req.Header.Set(relayConnectionHeader, "conn_s")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	got := make(chan string, 1)
	go func() {
		l, _ := br.ReadString('\n')
		got <- l
	}()
	select {
	case l := <-got:
		if l != "data: one\n" {
			t.Fatalf("first line %q", l)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first chunk was not delivered before the response finished: the relay buffers")
	}
	close(release)
	rest, _ := io.ReadAll(br)
	if !strings.Contains(string(rest), "data: two") {
		t.Fatalf("rest = %q", rest)
	}
}

func TestRelayConcurrentRequestsAndLargeBodies(t *testing.T) {
	cm := newRelayManager()
	addCluster(t, cm, "conn_c", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Logf("upstream: request body read failed after %d bytes: %v", len(b), err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Write(b) // echo
	}))
	relay := startRelay(t, cm)

	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := bytes.Repeat([]byte{byte('a' + i%26)}, 200_000+i)
			req, _ := http.NewRequest("POST", relay.URL+"/echo", bytes.NewReader(payload))
			req.Header.Set(relayConnectionHeader, "conn_c")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()
			b, rerr := io.ReadAll(resp.Body)
			if rerr != nil || resp.StatusCode != 200 {
				errs <- fmt.Errorf("request %d: status %d, read err %v after %d bytes", i, resp.StatusCode, rerr, len(b))
				return
			}
			if !bytes.Equal(b, payload) {
				errs <- fmt.Errorf("request %d: echoed %d bytes, want %d", i, len(b), len(payload))
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestRelayCapsConcurrentStreamsPerConnection(t *testing.T) {
	old := relayMaxStreamsPerConn
	relayMaxStreamsPerConn = 2
	t.Cleanup(func() { relayMaxStreamsPerConn = old })

	hold := make(chan struct{})
	var inflight atomic.Int32
	cm := newRelayManager()
	addCluster(t, cm, "conn_cap", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inflight.Add(1)
		<-hold
	}))
	addCluster(t, cm, "conn_other", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	relay := startRelay(t, cm)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); relayGet(t, relay.URL, "conn_cap", "/", nil) }()
	}
	eventually(t, "two requests in flight", func() bool { return inflight.Load() == 2 })

	resp, _ := relayGet(t, relay.URL, "conn_cap", "/", nil)
	if resp.StatusCode != 429 {
		t.Fatalf("third concurrent request: %d, want 429", resp.StatusCode)
	}
	if resp, _ := relayGet(t, relay.URL, "conn_other", "/", nil); resp.StatusCode != 200 {
		t.Fatalf("another cluster was throttled: %d", resp.StatusCode)
	}
	close(hold)
	wg.Wait()
	if resp, _ := relayGet(t, relay.URL, "conn_cap", "/", nil); resp.StatusCode != 200 {
		t.Fatalf("slot was not released: %d", resp.StatusCode)
	}
}

// The tunnel dying under an in-flight request must surface as an error status
// promptly, not a hang.
func TestRelaySessionDeathMidRequest(t *testing.T) {
	started := make(chan struct{})
	cm := newRelayManager()
	c := addCluster(t, cm, "conn_die", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done() // never answers
	}))
	relay := startRelay(t, cm)

	type result struct {
		status int
		body   string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		req, _ := http.NewRequest("GET", relay.URL+"/slow", nil)
		req.Header.Set(relayConnectionHeader, "conn_die")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			done <- result{err: err}
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		done <- result{status: resp.StatusCode, body: string(b)}
	}()
	<-started
	c.agent.Close() // the cluster's end of the tunnel goes away

	select {
	case r := <-done:
		if r.err != nil || (r.status != 502 && r.status != 503) {
			t.Fatalf("got %+v, want 502/503", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request hung after the tunnel died")
	}

	// And the next one is told the cluster is not connected.
	eventually(t, "cloud notices the dead session", func() bool { return c.cloud.IsClosed() })
	if resp, _ := relayGet(t, relay.URL, "conn_die", "/", nil); resp.StatusCode != 503 {
		t.Fatalf("after death: %d, want 503", resp.StatusCode)
	}
}

func TestRelayResponseHeaderTimeout(t *testing.T) {
	old := relayResponseHeaderTimeout
	relayResponseHeaderTimeout = 200 * time.Millisecond
	t.Cleanup(func() { relayResponseHeaderTimeout = old })

	cm := newRelayManager()
	addCluster(t, cm, "conn_slow", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
		}
	}))
	relay := startRelay(t, cm)
	start := time.Now()
	resp, _ := relayGet(t, relay.URL, "conn_slow", "/", nil)
	if resp.StatusCode != 504 || time.Since(start) > 2*time.Second {
		t.Fatalf("status %d after %v, want 504 quickly", resp.StatusCode, time.Since(start))
	}
}

// An agent that does not accept the stream (remote access disabled there, or
// its stream cap reached) closes it; the relay must answer 502, not hang.
func TestRelayAgentRefusesStream(t *testing.T) {
	cm := newRelayManager()
	cloud, agent := yamuxPair(t)
	go conn_client.ServeRemoteStreams(context.Background(), agent, func() string { return "" }) // not configured
	if err := cm.registerTunnel("conn_off", &LiveConnection{YamuxSess: cloud}); err != nil {
		t.Fatal(err)
	}
	relay := startRelay(t, cm)
	resp, _ := relayGet(t, relay.URL, "conn_off", "/", nil)
	if resp.StatusCode != 502 {
		t.Fatalf("status %d, want 502", resp.StatusCode)
	}
}

func TestRelayServerHealthAndHeaderLimit(t *testing.T) {
	cm := newRelayManager()
	addCluster(t, cm, "conn_hl", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "orchestrator health")
	}))
	relay := newRemoteRelay(cm, "127.0.0.1:0")
	if err := relay.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		relay.Shutdown(ctx)
	})
	base := "http://" + relay.Addr().String()

	// Direct probe: answered by the relay.
	if resp, body := relayGet(t, base, "", "/health", nil); resp.StatusCode != 200 || !strings.Contains(body, "ok") {
		t.Fatalf("health = %d %q", resp.StatusCode, body)
	}
	// Proxied traffic for the same path belongs to the cluster.
	if _, body := relayGet(t, base, "conn_hl", "/health", map[string]string{assertion.HeaderName: "a.b.c"}); body != "orchestrator health" {
		t.Fatalf("proxied /health = %q", body)
	}
	// Oversized headers are refused before any routing.
	req, _ := http.NewRequest("GET", base+"/", nil)
	req.Header.Set(relayConnectionHeader, "conn_hl")
	req.Header.Set("X-Big", strings.Repeat("a", 64<<10))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Fatalf("oversized headers: %d", resp.StatusCode)
	}
}

func TestRelayRejectsConnectMethod(t *testing.T) {
	cm := newRelayManager()
	addCluster(t, cm, "conn_m", http.NotFoundHandler())
	relay := startRelay(t, cm)
	c, err := net.Dial("tcp", strings.TrimPrefix(relay.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprintf(c, "CONNECT 10.0.0.1:22 HTTP/1.1\r\nHost: 10.0.0.1:22\r\n%s: conn_m\r\n\r\n", relayConnectionHeader)
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 405 {
		t.Fatalf("CONNECT: %d", resp.StatusCode)
	}
}

func TestScrubRelayHeadersKeepsOnlyTheAssertionUnderUnderleafPrefix(t *testing.T) {
	h := http.Header{}
	h.Set(assertion.HeaderName, "x")
	h.Set("x-underleaf-other", "y")
	h.Set("Accept", "z")
	scrubRelayHeaders(h)
	if h.Get(assertion.HeaderName) != "x" || h.Get("X-Underleaf-Other") != "" || h.Get("Accept") != "z" {
		t.Fatalf("%v", h)
	}
}

func writeConfig(t *testing.T, yaml string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "conn_worker.yaml")
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNDERLEAF_CONFIG", p)
}

func TestRelayPortConfig(t *testing.T) {
	t.Setenv("UNDERLEAF_CONFIG", "")
	if got := utils.GetConfig(utils.ConnWorkerConfig).Connections.RemoteRelayPort; got != 9071 {
		t.Fatalf("default = %d, want 9071", got)
	}
	// A gateway: block written before the field existed keeps the default.
	writeConfig(t, "gateway:\n  gateway_port: 9020\n  node_connections_port: 9021\n  domain: x.test\n")
	cfg := utils.GetConfig(utils.ConnWorkerConfig)
	if cfg.Connections.RemoteRelayPort != 9071 || cfg.Connections.Domain != "x.test" {
		t.Fatalf("legacy block: port=%d domain=%q", cfg.Connections.RemoteRelayPort, cfg.Connections.Domain)
	}
	writeConfig(t, "gateway:\n  gateway_port: 9020\n  node_connections_port: 9021\n  remote_relay_port: 9555\n  domain: x.test\n")
	if got := utils.GetConfig(utils.ConnWorkerConfig).Connections.RemoteRelayPort; got != 9555 {
		t.Fatalf("configured = %d, want 9555", got)
	}
}
