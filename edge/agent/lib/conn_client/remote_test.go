package conn_client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/ambientlabscomputing/underleaf_v2/shared/assertion"
)

// tunnel returns the cloud-side (yamux server) and agent-side (client) sessions
// of a real TCP connection, like conn_worker and the agent.
func tunnel(t *testing.T) (cloud, agent *yamux.Session) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	type res struct {
		s   *yamux.Session
		err error
	}
	ch := make(chan res, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			ch <- res{nil, err}
			return
		}
		s, err := yamux.Server(c, nil)
		ch <- res{s, err}
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	agent, err = yamux.Client(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := <-ch
	if r.err != nil {
		t.Fatal(r.err)
	}
	t.Cleanup(func() { r.s.Close(); agent.Close() })
	return r.s, agent
}

// countingListener is a fake Orchestrator remote listener that records how many
// connections it received.
func fakeRemoteListener(t *testing.T, h http.Handler) (addr string, conns *atomic.Int32) {
	t.Helper()
	conns = new(atomic.Int32)
	srv := httptest.NewUnstartedServer(h)
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String(), conns
}

func TestAgentSplicesCloudOpenedStreamToTheConfiguredTargetOnly(t *testing.T) {
	var gotHeader atomic.Value
	addr, conns := fakeRemoteListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader.Store(r.Header.Get(assertion.HeaderName))
		fmt.Fprint(w, "from-orchestrator")
	}))
	cloud, agent := tunnel(t)
	go ServeRemoteStreams(context.Background(), agent, func() string { return addr })

	s, err := cloud.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fmt.Fprint(s, assertion.StreamPreamble)
	fmt.Fprintf(s, "GET / HTTP/1.1\r\nHost: x\r\n%s: tok123\r\nConnection: close\r\n\r\n", assertion.HeaderName)
	resp, err := http.ReadResponse(bufio.NewReader(s), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "from-orchestrator" || gotHeader.Load() != "tok123" {
		t.Fatalf("body=%q header=%v", body, gotHeader.Load())
	}
	if conns.Load() != 1 {
		t.Fatalf("remote listener saw %d connections", conns.Load())
	}
}

func expectClosed(t *testing.T, s net.Conn) {
	t.Helper()
	_ = s.SetReadDeadline(time.Now().Add(3 * time.Second))
	if n, err := s.Read(make([]byte, 16)); err == nil || n > 0 {
		t.Fatalf("expected the agent to close the stream, read n=%d err=%v", n, err)
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("agent left the stream open")
	}
}

func TestAgentRefusesStreamsWithoutThePreamble(t *testing.T) {
	old := preambleTimeout
	preambleTimeout = 300 * time.Millisecond
	defer func() { preambleTimeout = old }()
	addr, conns := fakeRemoteListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	cloud, agent := tunnel(t)
	go ServeRemoteStreams(context.Background(), agent, func() string { return addr })

	for _, payload := range []string{
		"GET / HTTP/1.1\r\nHost: x\r\n\r\n", // plain http
		"UNDERLEAF-REMOTE/2\n",              // wrong version
		"UNDERLEAF-REM",                     // preamble never completes
		"stream_abc123\n",                   // gateway-style stream id
	} {
		s, err := cloud.Open()
		if err != nil {
			t.Fatal(err)
		}
		_, _ = s.Write([]byte(payload))
		expectClosed(t, s) // including a preamble that never completes (timeout)
		s.Close()
	}
	time.Sleep(100 * time.Millisecond)
	if conns.Load() != 0 {
		t.Fatalf("remote listener was contacted %d time(s) for non-remote streams", conns.Load())
	}
}

func TestAgentCannotBeSteeredToAnotherPort(t *testing.T) {
	addr, conns := fakeRemoteListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	decoy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer decoy.Close()
	var decoyHits atomic.Int32
	go func() {
		for {
			c, err := decoy.Accept()
			if err != nil {
				return
			}
			decoyHits.Add(1)
			c.Close()
		}
	}()

	cloud, agent := tunnel(t)
	go ServeRemoteStreams(context.Background(), agent, func() string { return addr })
	s, _ := cloud.Open()
	defer s.Close()
	// The stream tries to name a different destination; it is only more bytes.
	fmt.Fprintf(s, "%s%s\nGET / HTTP/1.1\r\nHost: %s\r\n\r\n", assertion.StreamPreamble, decoy.Addr(), decoy.Addr())
	time.Sleep(200 * time.Millisecond)
	if decoyHits.Load() != 0 {
		t.Fatal("the agent connected to a destination named by the stream")
	}
	if conns.Load() != 1 {
		t.Fatalf("configured target saw %d connections, want 1", conns.Load())
	}
}

func TestAgentRefusesEverythingWhenRemoteAccessIsNotConfigured(t *testing.T) {
	cloud, agent := tunnel(t)
	go ServeRemoteStreams(context.Background(), agent, func() string { return "" })
	s, _ := cloud.Open()
	defer s.Close()
	fmt.Fprint(s, assertion.StreamPreamble)
	expectClosed(t, s)
}

func TestAgentClosesStreamWhenTheRemoteListenerIsDown(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := l.Addr().String()
	l.Close()
	cloud, agent := tunnel(t)
	go ServeRemoteStreams(context.Background(), agent, func() string { return dead })
	s, _ := cloud.Open()
	defer s.Close()
	fmt.Fprint(s, assertion.StreamPreamble)
	expectClosed(t, s)
}

func TestSetRemoteTargetOnConnClient(t *testing.T) {
	cc := &ConnClient{}
	if cc.remoteTargetAddr() != "" {
		t.Fatal("default target must be empty (remote access off)")
	}
	cc.SetRemoteTarget("127.0.0.1:50110")
	if cc.remoteTargetAddr() != "127.0.0.1:50110" {
		t.Fatal("SetRemoteTarget")
	}
}
