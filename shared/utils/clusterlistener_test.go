package utils_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/peer"

	"github.com/ambientlabscomputing/underleaf_v2/shared/pki"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

const nodeA = "node_d0abcdefghij0123456k"

type clusterFixture struct {
	ca        *pki.CA
	addr      string
	joinHits  atomic.Int32
	grpcHits  atomic.Int32
	revoked   atomic.Value // map[string]bool
	lastPeer  atomic.Value // string
	trustPool *x509.CertPool
}

func (f *clusterFixture) isRevoked(serial string) bool {
	m, _ := f.revoked.Load().(map[string]bool)
	return m[serial]
}

func newClusterFixture(t *testing.T) *clusterFixture {
	t.Helper()
	f := &clusterFixture{}
	ca, err := pki.NewCA("test CA", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	f.ca = ca
	f.trustPool, _ = pki.CertPool(ca.CertPEM)
	src := pki.NewServerCertSource(ca, []string{"127.0.0.1"}, nil)
	mtls, err := pki.ClusterServerTLS(src, ca.CertPEM, f.isRevoked)
	if err != nil {
		t.Fatal(err)
	}
	cl, err := utils.ListenCluster("127.0.0.1:0", mtls, pki.JoinServerTLS(src))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cl.Close() })
	f.addr = cl.Addr().String()

	gs := grpc.NewServer(grpc.Creds(utils.ClusterGRPCCredentials()), grpc.UnaryInterceptor(
		func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
			f.grpcHits.Add(1)
			if p, ok := peer.FromContext(ctx); ok {
				if ti, ok := p.AuthInfo.(credentials.TLSInfo); ok {
					f.lastPeer.Store(ti.State.PeerCertificates[0].Subject.CommonName)
				}
			}
			return h(ctx, req)
		}))
	healthpb.RegisterHealthServer(gs, health.NewServer())
	go gs.Serve(cl.GRPC)
	t.Cleanup(gs.Stop)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/join", func(w http.ResponseWriter, r *http.Request) { f.joinHits.Add(1); w.Write([]byte("ok")) })
	hs := &http.Server{Handler: mux, ErrorLog: log.New(io.Discard, "", 0)}
	go hs.Serve(cl.Join)
	t.Cleanup(func() { hs.Close() })
	return f
}

func (f *clusterFixture) nodeCreds(t *testing.T, ca *pki.CA, id string) *tls.Config {
	t.Helper()
	key, csrPEM, err := pki.GenerateNodeKey(id)
	if err != nil {
		t.Fatal(err)
	}
	csr, _ := pki.ParseCSR(csrPEM)
	nc, err := ca.SignNode(csr, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	keyPEM, _ := pki.MarshalKeyPEM(key)
	cert, err := tls.X509KeyPair(append(keyPEM, nc.PEM...), append(keyPEM, nc.PEM...))
	if err != nil {
		t.Fatal(err)
	}
	cert.Leaf = nc.Cert
	cfg, err := pki.NodeClientTLS(f.ca.CertPEM, func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return &cert, nil })
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func (f *clusterFixture) check(t *testing.T, cfg *tls.Config) error {
	t.Helper()
	conn, err := grpc.NewClient(f.addr, grpc.WithTransportCredentials(credentials.NewTLS(cfg)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	return err
}

func TestClusterListenerMTLSClientReachesGRPCAndIsIdentified(t *testing.T) {
	f := newClusterFixture(t)
	if err := f.check(t, f.nodeCreds(t, f.ca, nodeA)); err != nil {
		t.Fatalf("member call failed: %v", err)
	}
	if got, _ := f.lastPeer.Load().(string); got != nodeA {
		t.Errorf("handler saw peer %q, want %q (the verified certificate CN)", got, nodeA)
	}
}

func TestClusterListenerRefusesClientWithoutCertificate(t *testing.T) {
	f := newClusterFixture(t)
	cfg := &tls.Config{RootCAs: f.trustPool, ServerName: pki.ServerName, MinVersion: tls.VersionTLS13}
	if err := f.check(t, cfg); err == nil {
		t.Fatal("a gRPC client with no client certificate was served")
	}
	if f.grpcHits.Load() != 0 {
		t.Fatalf("a handler ran %d time(s) for an unauthenticated client", f.grpcHits.Load())
	}
}

func TestClusterListenerRefusesClientCertFromForeignCA(t *testing.T) {
	f := newClusterFixture(t)
	foreign, _ := pki.NewCA("foreign", time.Now())
	if err := f.check(t, f.nodeCreds(t, foreign, nodeA)); err == nil {
		t.Fatal("a client certificate from another CA was accepted")
	}
	if f.grpcHits.Load() != 0 {
		t.Fatal("handler ran for a foreign-CA client")
	}
}

func TestClusterListenerEnforcesDenyList(t *testing.T) {
	f := newClusterFixture(t)
	cfg := f.nodeCreds(t, f.ca, nodeA)
	if err := f.check(t, cfg); err != nil {
		t.Fatal(err)
	}
	cert, _ := cfg.GetClientCertificate(nil)
	f.revoked.Store(map[string]bool{pki.SerialString(cert.Leaf): true})
	if err := f.check(t, cfg); err == nil {
		t.Fatal("a revoked certificate was still accepted")
	}
}

func TestClusterJoinSideServesOnlyHTTPAndNeverGRPC(t *testing.T) {
	f := newClusterFixture(t)
	// A plain HTTPS client with no certificate (what a joining node is) reaches
	// the join handler...
	hc := &http.Client{Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: f.trustPool, ServerName: pki.ServerName, MinVersion: tls.VersionTLS13},
		ForceAttemptHTTP2: false,
		TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
	}}
	resp, err := hc.Post("https://"+f.addr+"/v1/join", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || f.joinHits.Load() != 1 {
		t.Fatalf("join status=%d hits=%d", resp.StatusCode, f.joinHits.Load())
	}
	// ... and nothing else is routable there.
	resp, err = hc.Get("https://" + f.addr + "/grpc.health.v1.Health/Check")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("non-join path status = %d, want 404", resp.StatusCode)
	}
	if f.grpcHits.Load() != 0 {
		t.Fatal("gRPC handler reachable from the join side")
	}
	// An HTTP/2 client with no certificate must fail the handshake (it asks
	// for h2, which is the mTLS side).
	h2 := &http.Client{Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: f.trustPool, ServerName: pki.ServerName, MinVersion: tls.VersionTLS13, NextProtos: []string{"h2", "http/1.1"}},
		ForceAttemptHTTP2: true,
	}}
	if resp, err := h2.Post("https://"+f.addr+"/v1/join", "application/json", nil); err == nil {
		resp.Body.Close()
		t.Fatalf("h2 client without certificate was served (status %d)", resp.StatusCode)
	}
	if f.joinHits.Load() != 1 {
		t.Errorf("join handler hit by the h2 client")
	}
}

func TestClusterListenerPlainTCPAndGarbageAreDropped(t *testing.T) {
	f := newClusterFixture(t)
	c, err := net.Dial("tcp", f.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte("GET / HTTP/1.1\r\n\r\n"))
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, _ := c.Read(buf)
	if n > 0 && string(buf[:4]) == "HTTP" {
		t.Fatalf("plaintext HTTP was answered on the cluster port: %q", buf[:n])
	}
	if f.joinHits.Load() != 0 || f.grpcHits.Load() != 0 {
		t.Fatal("handler reached by plaintext client")
	}
}

func TestListenClusterRefusesWeakTLSConfigs(t *testing.T) {
	ca, _ := pki.NewCA("c", time.Now())
	src := pki.NewServerCertSource(ca, nil, nil)
	good, _ := pki.ClusterServerTLS(src, ca.CertPEM, nil)
	join := pki.JoinServerTLS(src)

	mutate := func(f func(c *tls.Config)) *tls.Config {
		c := good.Clone()
		f(c)
		return c
	}
	for name, mtls := range map[string]*tls.Config{
		"no client auth":         mutate(func(c *tls.Config) { c.ClientAuth = tls.NoClientCert }),
		"client cert optional":   mutate(func(c *tls.Config) { c.ClientAuth = tls.VerifyClientCertIfGiven }),
		"any client cert":        mutate(func(c *tls.Config) { c.ClientAuth = tls.RequireAnyClientCert }),
		"request without verify": mutate(func(c *tls.Config) { c.ClientAuth = tls.RequestClientCert }),
		"no CA pool":             mutate(func(c *tls.Config) { c.ClientCAs = nil }),
		"self-swapping": mutate(func(c *tls.Config) {
			c.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) { return nil, nil }
		}),
		"nil": nil,
	} {
		if cl, err := utils.ListenCluster("127.0.0.1:0", mtls, join); err == nil {
			cl.Close()
			t.Errorf("%s: ListenCluster accepted an mTLS config that does not require client certs", name)
		}
	}
	for name, j := range map[string]*tls.Config{
		"join may negotiate h2": func() *tls.Config { c := join.Clone(); c.NextProtos = []string{"h2", "http/1.1"}; return c }(),
		"join requests certs":   func() *tls.Config { c := join.Clone(); c.ClientAuth = tls.RequestClientCert; return c }(),
		"join h2 only":          func() *tls.Config { c := join.Clone(); c.NextProtos = []string{"h2"}; return c }(),
		"nil":                   nil,
	} {
		if cl, err := utils.ListenCluster("127.0.0.1:0", good, j); err == nil {
			cl.Close()
			t.Errorf("%s: ListenCluster accepted a join config that could reach gRPC", name)
		}
	}
	cl, err := utils.ListenCluster("127.0.0.1:0", good, join)
	if err != nil {
		t.Fatalf("good configs refused: %v", err)
	}
	cl.Close()
}
