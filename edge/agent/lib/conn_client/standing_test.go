package conn_client

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/assertion"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// sinkConn stands in for the raw TLS connection: the handshake line goes
// nowhere and closing it is harmless.
type sinkConn struct{ net.Conn }

func (sinkConn) Write(b []byte) (int, error) { return len(b), nil }
func (sinkConn) Close() error                { return nil }

// testCertDir writes the files NewConnClient loads (an agent's node cert, key
// and CA chain). The certificate content is irrelevant: nothing dials.
func testCertDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	for name, data := range map[string][]byte{
		string(types.CertFileNameNodeCert):   certPEM,
		string(types.CertFileNameCAChain):    certPEM,
		string(types.CertFileNameClusterKey): pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func newTestClient(t *testing.T, opts ...Option) *ConnClient {
	t.Helper()
	cc, err := NewConnClient(utils.Config{ConfigType: utils.AgentConfig, CertDir: testCertDir(t)}, make(chan struct{}), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cc.cancel)
	return cc
}

func remoteRequest(t *testing.T, cloudSide net.Conn) (string, error) {
	t.Helper()
	fmt.Fprint(cloudSide, assertion.StreamPreamble)
	fmt.Fprintf(cloudSide, "GET / HTTP/1.1\r\nHost: x\r\n%s: tok\r\nConnection: close\r\n\r\n", assertion.HeaderName)
	_ = cloudSide.SetReadDeadline(time.Now().Add(3 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(cloudSide), nil)
	if err != nil {
		return "", err
	}
	b, _ := io.ReadAll(resp.Body)
	return string(b), nil
}

// The gateway client must never serve a cloud-opened stream, even when
// UNDERLEAF_REMOTE_TARGET is set in its environment: only the dedicated remote
// client does.
func TestGatewayClientRefusesCloudOpenedStreams(t *testing.T) {
	addr, conns := fakeRemoteListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "from-orchestrator")
	}))
	t.Setenv(utils.EnvRemoteTarget, addr) // the old behaviour armed the gateway client from here

	gw := newTestClient(t)
	if gw.remoteTargetAddr() != "" {
		t.Fatalf("gateway client has remote target %q", gw.remoteTargetAddr())
	}
	cloud, agent := tunnel(t)
	if _, err := gw.adopt(context.Background(), "conn_gw", sinkConn{}, agent); err != nil {
		t.Fatal(err)
	}
	s, err := cloud.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fmt.Fprint(s, assertion.StreamPreamble)
	expectClosed(t, s)
	time.Sleep(100 * time.Millisecond)
	if conns.Load() != 0 {
		t.Fatalf("the gateway client connected a cloud-opened stream to the remote listener (%d)", conns.Load())
	}
}

func TestRemoteClientServesCloudOpenedStreams(t *testing.T) {
	addr, conns := fakeRemoteListener(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "from-orchestrator")
	}))
	rc := newTestClient(t, WithRemoteTarget(addr))
	cloud, agent := tunnel(t)
	if _, err := rc.adopt(context.Background(), "conn_remote", sinkConn{}, agent); err != nil {
		t.Fatal(err)
	}
	s, err := cloud.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	body, err := remoteRequest(t, s)
	if err != nil || body != "from-orchestrator" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	if conns.Load() != 1 {
		t.Fatalf("remote listener saw %d connections", conns.Load())
	}
}

func bareClient(t *testing.T) *ConnClient {
	t.Helper()
	// A client that cannot dial (no certificates): its loop just retries, which
	// is all SetConnection's bookkeeping needs.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &ConnClient{
		ctx: ctx, cancel: cancel, streams: make(LiveStreams),
		config: utils.Config{ConnectionsClient: &utils.ConnectionsClient{Host: "127.0.0.1", Port: 1}},
	}
}

func TestSetConnectionIsIdempotentAndSwitchesCleanly(t *testing.T) {
	cc := bareClient(t)
	if cc.StandingConnectionID() != "" {
		t.Fatal("no standing connection yet")
	}
	if !cc.SetConnection("conn_a") {
		t.Fatal("first SetConnection must start a loop")
	}
	first := cc.loop
	for i := 0; i < 3; i++ {
		if cc.SetConnection("conn_a") {
			t.Fatal("the same id must not restart the loop")
		}
	}
	if cc.loop != first {
		t.Fatal("the loop was replaced for the same id")
	}
	select {
	case <-first.done:
		t.Fatal("the loop died on its own")
	default:
	}

	if !cc.SetConnection("conn_b") {
		t.Fatal("a new id must switch")
	}
	select {
	case <-first.done: // the old loop is fully stopped before SetConnection returned
	default:
		t.Fatal("the old loop is still running after the switch")
	}
	if cc.StandingConnectionID() != "conn_b" {
		t.Fatalf("standing = %q", cc.StandingConnectionID())
	}
	if cc.SetConnection("conn_b") {
		t.Fatal("conn_b is already standing")
	}
	// Switching back works too.
	if !cc.SetConnection("conn_a") || cc.StandingConnectionID() != "conn_a" {
		t.Fatal("switch back")
	}
}

func TestSetConnectionSwitchClosesTheOldConnection(t *testing.T) {
	cc := bareClient(t)
	cloud, agent := tunnel(t)
	_ = cloud
	// Pretend the first loop's connection is established.
	cc.SetConnection("conn_a")
	old := cc.loop
	cc.mu.Lock()
	cc.connection = &LiveConnection{ConnID: "conn_a", Conn: sinkConn{}, Multiplexer: agent}
	cc.mu.Unlock()
	_ = old

	cc.SetConnection("conn_b")
	// Give the (cancelled) loop's cleanup time to run; SetConnection waited for it already.
	if !agent.IsClosed() {
		t.Fatal("the old connection's session was left open after a switch")
	}
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	if cc.connection != nil && cc.connection.ConnID == "conn_a" {
		t.Fatal("the old connection is still the active one")
	}
}
