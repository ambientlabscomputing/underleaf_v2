package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/ambientlabscomputing/underleaf_v2/cloud/conn_worker/repository"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/lib/conn_client"
	"github.com/ambientlabscomputing/underleaf_v2/shared/assertion"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

const testDomain = "example.test"

// fakeStore is an in-memory connStore.
type fakeStore struct {
	mu sync.Mutex
	m  map[string]string
}

func newFakeStore() *fakeStore { return &fakeStore{m: map[string]string{}} }

func (f *fakeStore) Get(_ context.Context, key string) (repository.RepositoryResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.m[key]
	if !ok {
		return "", errors.New("redis: nil")
	}
	return repository.RepositoryResult(v), nil
}

func (f *fakeStore) Set(_ context.Context, key string, value interface{}, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[key] = fmt.Sprint(value)
	return nil
}

func (f *fakeStore) putConnection(id string, state types.ConnectionState) {
	c := types.Connection{ID: id, State: state, Status: types.StatusSucceeded}
	_ = f.Set(context.Background(), id, string(c.ToJSON()), 0)
}

func (f *fakeStore) connectionState(t *testing.T, id string) types.ConnectionState {
	t.Helper()
	r, err := f.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	var c types.Connection
	if err := r.Parse(&c); err != nil {
		t.Fatal(err)
	}
	return c.State
}

// newTestManager returns a ConnectionManager accepting tunnel dials on a real
// loopback listener (the role nginx's stream proxy plays in production).
func newTestManager(t *testing.T) (*ConnectionManager, *fakeStore) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	store := newFakeStore()
	cm := &ConnectionManager{
		LiveTunnels: map[string]*LiveConnection{},
		LiveStreams: map[string]*LiveStream{},
		listener:    ln,
		repository:  store,
		gwChan:      make(chan StreamHandlerReq, 8),
		config:      utils.Config{Connections: &utils.ConnectionsConfig{Domain: testDomain}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	go cm.Serve(ctx)
	t.Cleanup(func() {
		cancel()
		ln.Close()
		cm.mu.Lock()
		for _, lc := range cm.LiveTunnels {
			lc.YamuxSess.Close()
		}
		cm.mu.Unlock()
	})
	return cm, store
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

// agentDial does what edge/agent/lib/conn_client.establish does after TLS:
// yamux.Client on the raw connection, then "<id>\n" written on the raw
// connection.
func agentDial(t *testing.T, addr, id string) (*yamux.Session, net.Conn) {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := yamux.Client(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write([]byte(id + "\n")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close(); c.Close() })
	return sess, c
}

// rawHandshake sends only the handshake line (no yamux client), so the test
// can watch what the server does with the connection.
func rawHandshake(t *testing.T, addr, id string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if _, err := c.Write([]byte(id + "\n")); err != nil {
		t.Fatal(err)
	}
	return c
}

func expectClosedByServer(t *testing.T, c net.Conn) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("server did not close the connection (err=%v)", err)
	}
}

func TestValidConnID(t *testing.T) {
	good := []string{"conn_d1a2b3c4d5e6f7a8b9c0", "conn_a-b_C9"}
	bad := []string{"", "conn_", "conn", "node_abc", "stream_abc", "conn_a b", "conn_a\nb", "conn_a/b", "conn_a:b",
		"conn_" + string(make([]byte, 200)), "CONN_abc", "conn_é"}
	for _, id := range good {
		if !validConnID(id) {
			t.Errorf("validConnID(%q) = false", id)
		}
	}
	for _, id := range bad {
		if validConnID(id) {
			t.Errorf("validConnID(%q) = true", id)
		}
	}
}

// The handshake reader consumes only the handshake: bytes that arrived behind
// it (the first yamux frames) must still be readable.
func TestReadHandshakeKeepsBytesBehindIt(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go a.Write([]byte("conn_abc123\nLEFTOVER"))

	id, br, err := readHandshake(b, testDomain)
	if err != nil || id != "conn_abc123" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	got := make([]byte, len("LEFTOVER"))
	if _, err := io.ReadFull(br, got); err != nil || string(got) != "LEFTOVER" {
		t.Fatalf("leftover = %q, %v", got, err)
	}
}

func TestReadHandshakeRejectsGarbage(t *testing.T) {
	for name, in := range map[string]string{
		"no newline then close": "conn_abc",
		"not an id":             "hello world\n",
		"wrong prefix":          "node_abc\n",
		"http, wrong host":      "GET / HTTP/1.1\r\nHost: conn_abc.connect.other.org\r\n\r\n",
		"http, no id":           "GET / HTTP/1.1\r\nHost: .connect." + testDomain + "\r\n\r\n",
		"too long":              string(make([]byte, 5000)) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			a, b := net.Pipe()
			defer b.Close()
			go func() { a.Write([]byte(in)); a.Close() }()
			if id, _, err := readHandshake(b, testDomain); err == nil {
				t.Fatalf("accepted %q as %q", name, id)
			}
		})
	}
}

func TestReadHandshakeLegacyHostForm(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go a.Write([]byte("GET / HTTP/1.1\r\nHost: conn_xyz.connect." + testDomain + "\r\n\r\nYAMUX"))
	id, br, err := readHandshake(b, testDomain)
	if err != nil || id != "conn_xyz" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	got := make([]byte, 5)
	if _, err := io.ReadFull(br, got); err != nil || string(got) != "YAMUX" {
		t.Fatalf("leftover = %q, %v", got, err)
	}
}

func TestHandshakeAgentFormEstablishesTunnel(t *testing.T) {
	cm, store := newTestManager(t)
	store.putConnection("conn_one", types.ConnectionStateProvisioned) // first dial: provisioned
	agentDial(t, cm.listener.Addr().String(), "conn_one")

	eventually(t, "tunnel registered", func() bool { _, ok := cm.Tunnel("conn_one"); return ok })
	eventually(t, "record promoted to running", func() bool {
		return store.connectionState(t, "conn_one") == types.ConnectionStateRunning
	})
}

func TestHandshakeLegacyHostFormStillWorks(t *testing.T) {
	cm, store := newTestManager(t)
	store.putConnection("conn_legacy", types.ConnectionStateRunning)
	c, err := net.Dial("tcp", cm.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: conn_legacy.connect.%s\r\n\r\n", testDomain)
	sess, err := yamux.Client(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	eventually(t, "tunnel registered", func() bool { _, ok := cm.Tunnel("conn_legacy"); return ok })
}

func TestHandshakeRefusals(t *testing.T) {
	cm, store := newTestManager(t)
	store.putConnection("conn_closed", types.ConnectionStateClosed)
	for name, id := range map[string]string{
		"closed record":  "conn_closed",
		"unknown record": "conn_nope",
		"bad id":         "../etc/passwd",
	} {
		t.Run(name, func(t *testing.T) {
			expectClosedByServer(t, rawHandshake(t, cm.listener.Addr().String(), id))
			if _, ok := cm.Tunnel(id); ok {
				t.Fatal("tunnel registered")
			}
		})
	}
}

// Regression: the entry used to outlive its session, so the agent's redial was
// refused with "connection already exists".
func TestRedialAfterSessionEnds(t *testing.T) {
	cm, store := newTestManager(t)
	store.putConnection("conn_redial", types.ConnectionStateProvisioned)
	addr := cm.listener.Addr().String()

	s1, c1 := agentDial(t, addr, "conn_redial")
	eventually(t, "first tunnel", func() bool { _, ok := cm.Tunnel("conn_redial"); return ok })
	first, _ := cm.Tunnel("conn_redial")

	c1.Close() // network drop
	_ = s1
	eventually(t, "entry removed when the session ends", func() bool {
		cm.mu.RLock()
		defer cm.mu.RUnlock()
		return len(cm.LiveTunnels) == 0
	})

	agentDial(t, addr, "conn_redial")
	eventually(t, "second tunnel", func() bool {
		s, ok := cm.Tunnel("conn_redial")
		return ok && s != first
	})
}

// A half-open old session (the cloud has not noticed the drop yet) must not
// lock the agent out: it is probed, found dead, and replaced.
func TestRedialReplacesUnresponsiveSession(t *testing.T) {
	old := tunnelProbeTimeout
	tunnelProbeTimeout = 300 * time.Millisecond
	t.Cleanup(func() { tunnelProbeTimeout = old })

	cm, store := newTestManager(t)
	store.putConnection("conn_half", types.ConnectionStateRunning)
	addr := cm.listener.Addr().String()

	// "Agent" that handshakes and then goes silent: it never answers pings.
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprint(c, "conn_half\n")
	eventually(t, "first tunnel", func() bool { _, ok := cm.Tunnel("conn_half"); return ok })
	stale, _ := cm.Tunnel("conn_half")

	agentDial(t, addr, "conn_half")
	eventually(t, "stale session replaced", func() bool {
		s, ok := cm.Tunnel("conn_half")
		return ok && s != stale
	})
	if !stale.IsClosed() {
		t.Fatal("stale session was not closed")
	}
}

func TestLiveTunnelIsNotDisplacedByASecondDial(t *testing.T) {
	cm, store := newTestManager(t)
	store.putConnection("conn_live", types.ConnectionStateRunning)
	addr := cm.listener.Addr().String()

	agentDial(t, addr, "conn_live") // responsive: yamux answers pings
	eventually(t, "first tunnel", func() bool { _, ok := cm.Tunnel("conn_live"); return ok })
	first, _ := cm.Tunnel("conn_live")

	expectClosedByServer(t, rawHandshake(t, addr, "conn_live"))
	if s, ok := cm.Tunnel("conn_live"); !ok || s != first {
		t.Fatal("live tunnel was displaced")
	}
}

func TestRemoveTunnelOnlyRemovesTheSameSession(t *testing.T) {
	cm := &ConnectionManager{LiveTunnels: map[string]*LiveConnection{}, LiveStreams: map[string]*LiveStream{}}
	s1, _ := yamuxPair(t)
	s2, _ := yamuxPair(t)
	if err := cm.registerTunnel("conn_x", &LiveConnection{YamuxSess: s1}); err != nil {
		t.Fatal(err)
	}
	cm.removeTunnel("conn_x", s2) // someone else's session
	if _, ok := cm.Tunnel("conn_x"); !ok {
		t.Fatal("removed a session that was not the registered one")
	}
	cm.removeTunnel("conn_x", s1)
	if _, ok := cm.Tunnel("conn_x"); ok {
		t.Fatal("did not remove the registered session")
	}
}

// Run with -race: every access path to the two maps, concurrently.
func TestLiveMapsAreRaceFree(t *testing.T) {
	cm := &ConnectionManager{
		LiveTunnels: map[string]*LiveConnection{},
		LiveStreams: map[string]*LiveStream{},
		repository:  newFakeStore(),
		config:      utils.Config{Redis: &utils.RedisConfig{TTLSeconds: 1}},
	}
	sess, _ := yamuxPair(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("conn_%d", i%3)
			for j := 0; j < 200; j++ {
				_ = cm.registerTunnel(id, &LiveConnection{TunnelID: id, YamuxSess: sess})
				cm.Tunnel(id)
				cm.removeTunnel(id, sess)
				cm.mu.Lock()
				cm.LiveStreams[fmt.Sprintf("s%d", j%5)] = &LiveStream{}
				cm.mu.Unlock()
				cm.flushActiveStreamBytes(context.Background())
				_ = cm.CloseStream(context.Background(), "s1")
				_ = cm.CloseConnection(context.Background(), id)
			}
		}(i)
	}
	wg.Wait()
}

// yamuxPair returns the cloud-side (server) and agent-side (client) sessions of
// a real loopback TCP connection.
func yamuxPair(t *testing.T) (cloud, agent *yamux.Session) {
	t.Helper()
	cloud, agent, _ = yamuxPairConns(t)
	return
}

func yamuxPairConns(t *testing.T) (cloud, agent *yamux.Session, agentConn net.Conn) {
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
	agentConn, err = net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	agent, err = yamux.Client(agentConn, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := <-ch
	if r.err != nil {
		t.Fatal(r.err)
	}
	t.Cleanup(func() { r.s.Close(); agent.Close() })
	return r.s, agent, agentConn
}

// ---- the real agent, end to end -------------------------------------------

type testPKI struct {
	dir       string // agent CertDir
	serverTLS *tls.Config
}

func newCert(t *testing.T, tmpl, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if parent == nil {
		parent, parentKey = tmpl, key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func keyPEM(t *testing.T, k *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// newTestPKI writes the files the agent loads (node cert, key, CA chain) and
// returns the TLS config for the nginx-like terminator that demands them.
func newTestPKI(t *testing.T) testPKI {
	t.Helper()
	now := time.Now()
	ca, caKey, caPEM := newCert(t, &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}, nil, nil)
	_, srvKey, srvPEM := newCert(t, &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "server"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}, ca, caKey)
	_, cliKey, cliPEM := newCert(t, &x509.Certificate{
		SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "node_test"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}, ca, caKey)

	dir := t.TempDir()
	for name, data := range map[string][]byte{
		types.CertFileNameNodeCert.String():   cliPEM,
		types.CertFileNameClusterKey.String(): keyPEM(t, cliKey),
		types.CertFileNameCAChain.String():    caPEM,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	srvCert, err := tls.X509KeyPair(srvPEM, keyPEM(t, srvKey))
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return testPKI{dir: dir, serverTLS: &tls.Config{
		Certificates: []tls.Certificate{srvCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
	}}
}

// nginxStream emulates nginx's `stream { listen 19021 ssl; proxy_pass
// conn_worker:9021; }`: terminate mTLS, pass the plaintext bytes through. It
// returns its address and a function that severs every connection through it
// (a network drop).
func nginxStream(t *testing.T, pki testPKI, upstream string) (addr string, sever func()) {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", pki.serverTLS)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			up, err := net.Dial("tcp", upstream)
			if err != nil {
				c.Close()
				continue
			}
			mu.Lock()
			conns = append(conns, c, up)
			mu.Unlock()
			go func() { io.Copy(up, c); up.Close() }()
			go func() { io.Copy(c, up); c.Close() }()
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String(), func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
		conns = nil
	}
}

// The whole chain with the production agent code (conn_client.ConnClient:
// real TLS dial, real handshake, real ServeRemoteStreams) on one side and the
// real ConnectionManager and RemoteRelay on the other.
func TestEndToEndWithRealAgent(t *testing.T) {
	cm, store := newTestManager(t)
	store.putConnection("conn_e2e", types.ConnectionStateProvisioned)

	var seen struct {
		sync.Mutex
		assertion, auth, subject, host, path string
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Lock()
		seen.assertion = r.Header.Get(assertion.HeaderName)
		seen.auth = r.Header.Get("Authorization")
		seen.subject = r.Header.Get("X-Subject-Id")
		seen.host = r.Host
		seen.path = r.URL.RequestURI()
		seen.Unlock()
		fmt.Fprint(w, "hello from the orchestrator")
	}))
	defer upstream.Close()

	pki := newTestPKI(t)
	nginxAddr, sever := nginxStream(t, pki, cm.listener.Addr().String())
	host, portStr, _ := net.SplitHostPort(nginxAddr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	alive := make(chan struct{})
	cc, err := conn_client.NewConnClient(utils.Config{
		ConfigType:        utils.AgentConfig,
		CertDir:           pki.dir,
		ConnectionsClient: &utils.ConnectionsClient{Host: host, Port: utils.NamedPort(port)},
	}, alive)
	if err != nil {
		t.Fatal(err)
	}
	cc.SetRemoteTarget(upstream.Listener.Addr().String())
	cc.Start()
	t.Cleanup(func() { close(alive) }) // stops the reconnect loop
	cc.Connect(context.Background(), "conn_e2e")

	eventually(t, "agent tunnel established through TLS + handshake", func() bool {
		_, ok := cm.Tunnel("conn_e2e")
		return ok
	})
	if got := store.connectionState(t, "conn_e2e"); got != types.ConnectionStateRunning {
		t.Fatalf("record state = %s", got)
	}

	relay := httptest.NewServer(newRemoteRelay(cm, "").Handler())
	defer relay.Close()

	do := func() (*http.Response, string) {
		req, _ := http.NewRequest("GET", relay.URL+"/api/v1/things?a=1", nil)
		req.Header.Set(relayConnectionHeader, "conn_e2e")
		req.Header.Set(assertion.HeaderName, "signed.by.cloud")
		req.Header.Set("Authorization", "Bearer client-supplied")
		req.Header.Set("X-Subject-Id", "attacker")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}
	resp, body := do()
	if resp.StatusCode != 200 || body != "hello from the orchestrator" {
		t.Fatalf("got %d %q", resp.StatusCode, body)
	}
	seen.Lock()
	if seen.assertion != "signed.by.cloud" || seen.auth != "" || seen.subject != "" || seen.host != "127.0.0.1" || seen.path != "/api/v1/things?a=1" {
		t.Fatalf("upstream saw %+v", &seen)
	}
	seen.Unlock()

	// Network drop: the agent's own reconnect loop redials, the cloud accepts
	// the new session for the same id, and requests flow again.
	first, _ := cm.Tunnel("conn_e2e")
	sever()
	eventually(t, "agent re-established the tunnel after a drop", func() bool {
		s, ok := cm.Tunnel("conn_e2e")
		return ok && s != first
	})
	eventually(t, "requests flow again", func() bool {
		resp, body := do()
		return resp.StatusCode == 200 && body == "hello from the orchestrator"
	})
}
