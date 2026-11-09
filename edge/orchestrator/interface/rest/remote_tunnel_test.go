package rest

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/lib/conn_client"
	"github.com/ambientlabscomputing/underleaf_v2/shared/assertion"
)

// tunnelPair returns the cloud side and the agent side of a yamux session over
// a real TCP connection (conn_worker is the server, the agent the client).
func tunnelPair(t *testing.T) (cloud, agent *yamux.Session) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	srvCh := make(chan *yamux.Session, 1)
	go func() {
		c, _ := ln.Accept()
		s, _ := yamux.Server(c, nil)
		srvCh <- s
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	agent, _ = yamux.Client(c, nil)
	cloud = <-srvCh
	t.Cleanup(func() { cloud.Close(); agent.Close() })
	return cloud, agent
}

// cloudRequest plays the cloud forwarding one request through the tunnel: open a
// stream, write the preamble, send the HTTP request (with the given extra
// headers), read the response.
func cloudRequest(t *testing.T, cloud *yamux.Session, method, path, body string, hdr map[string]string) (int, string) {
	t.Helper()
	s, err := cloud.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_ = s.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprint(s, assertion.StreamPreamble)
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s HTTP/1.1\r\nHost: cloud.example\r\nConnection: close\r\nContent-Length: %d\r\n", method, path, len(body))
	for k, v := range hdr {
		fmt.Fprintf(&b, "%s: %s\r\n", k, v)
	}
	b.WriteString("\r\n" + body)
	if _, err := io.WriteString(s, b.String()); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(s), nil)
	if err != nil {
		t.Fatalf("no response through the tunnel: %v", err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

// The whole edge path: cloud-opened stream -> agent -> Orchestrator remote
// listener -> assertion check -> UI/API handlers.
func TestRemoteRequestsThroughTheAgentTunnel(t *testing.T) {
	e := newRemoteEnv(t, true)
	cloud, agent := tunnelPair(t)
	target := strings.TrimPrefix(e.remote.URL, "http://")
	go conn_client.ServeRemoteStreams(context.Background(), agent, func() string { return target })

	tok := func(scopes ...string) map[string]string { return asr(e.cloud.mint(t, nil, scopes...)) }

	if code, body := cloudRequest(t, cloud, "GET", "/", "", tok("read")); code != 200 || !strings.Contains(body, "orchestrator-ui") {
		t.Fatalf("UI through the tunnel: %d %q", code, body)
	}
	if code, body := cloudRequest(t, cloud, "GET", "/api/v1/containers/abc/logs", "", tok("read")); code != 200 || !strings.Contains(body, `"hi"`) {
		t.Fatalf("API through the tunnel: %d %q", code, body)
	}
	// Negative paths through the same tunnel.
	if code, _ := cloudRequest(t, cloud, "GET", "/api/v1/health", "", nil); code != 401 {
		t.Fatalf("no assertion through the tunnel: %d, want 401", code)
	}
	if code, _ := cloudRequest(t, cloud, "GET", "/api/v1/health", "", map[string]string{"X-Subject-Id": testCluster}); code != 401 {
		t.Fatalf("forwarded identity header through the tunnel: %d, want 401", code)
	}
	replay := tok("read")
	if code, _ := cloudRequest(t, cloud, "GET", "/api/v1/health", "", replay); code != 200 {
		t.Fatalf("first use: %d", code)
	}
	if code, _ := cloudRequest(t, cloud, "GET", "/api/v1/health", "", replay); code != 401 {
		t.Fatalf("replay through the tunnel: %d, want 401", code)
	}
	if code, _ := cloudRequest(t, cloud, "POST", "/api/v1/deployments", `{"source":"gh:a/b"}`, merge(tok("read"), map[string]string{"Content-Type": "application/json"})); code != 403 {
		t.Fatalf("read-scoped deploy through the tunnel: %d, want 403", code)
	}
}

// If remote traffic is misdirected to the LOCAL listener (here: the agent's
// target is wrongly set to it), the local listener refuses it instead of
// treating it as the trusted local operator.
func TestMisdirectedRemoteTrafficFailsClosedOnTheLocalListener(t *testing.T) {
	e := newRemoteEnv(t, true)
	cloud, agent := tunnelPair(t)
	target := strings.TrimPrefix(e.local.URL, "http://")
	go conn_client.ServeRemoteStreams(context.Background(), agent, func() string { return target })

	// With a (valid) assertion header: refused by rejectRemoteCredentials.
	if code, _ := cloudRequest(t, cloud, "GET", "/api/v1/containers/abc/logs", "", asr(e.cloud.mint(t, nil, "admin"))); code != 403 {
		t.Fatalf("assertion-bearing request on the local listener: %d, want 403", code)
	}
}
