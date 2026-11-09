package cluster

import (
	"context"
	"crypto/tls"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	grpc_public "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	_ "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository/migrations"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/pki"
)

const (
	worker1 = "node_d0abcdefghij0123456k"
	worker2 = "node_d1abcdefghij0123456k"
)

type testBackend struct {
	cs   *service.ClusterService
	repo *repository.Repository
}

func (b *testBackend) Cluster() *service.ClusterService { return b.cs }
func (b *testBackend) Health() *service.HealthService   { return &service.HealthService{} }
func (b *testBackend) Containers() *service.ContainerService {
	return &service.ContainerService{Repository: b.repo}
}

type env struct {
	srv  *Server
	be   *testBackend
	addr string
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	repo, err := repository.NewRepositoryAt(filepath.Join(dir, "orch.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	port := freePort(t)
	cs := service.NewClusterService(repo, filepath.Join(dir, "ca"), port)
	be := &testBackend{cs: cs, repo: repo}
	srv := &Server{Service: be, Bind: net.JoinHostPort("127.0.0.1", port)}
	cs.StartListener = srv.Start
	t.Cleanup(srv.Close)
	return &env{srv: srv, be: be, addr: net.JoinHostPort("127.0.0.1", port)}
}

// joinNode runs the real worker-side protocol against the real listener and
// returns the persisted Member.
func (e *env) joinNode(t *testing.T, joinString, nodeID string) *pki.Member {
	t.Helper()
	info, err := pki.ParseJoinString(joinString)
	if err != nil {
		t.Fatal(err)
	}
	res, err := pki.Join(context.Background(), info, nodeID, pki.JoinNode{Name: nodeID, IPAddr: "10.0.0.2", OS: "linux", Arch: "arm64"})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	m, err := pki.SaveMember(filepath.Join(t.TempDir(), "node"), pki.MemberState{ManagerAddr: info.Addr, NodeID: nodeID, CAFingerprint: pki.FormatFingerprint(info.Fingerprint)}, res.Key, res.CertPEM, res.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func ping(ctx context.Context, conn grpc.ClientConnInterface) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := grpc_public.NewOrchestratorPublicClient(conn).Ping(ctx, &grpc_public.PingRequest{Source: "test"})
	return err
}

func TestFullJoinThenMTLS(t *testing.T) {
	e := newEnv(t)
	res, err := e.be.cs.AddNode(e.addr)
	if err != nil {
		t.Fatal(err)
	}
	m := e.joinNode(t, res.JoinString, worker1)

	conn, err := clients.DialCluster(m)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := ping(context.Background(), conn); err != nil {
		t.Fatalf("member ping over mTLS: %v", err)
	}
	// The node row exists and is a cluster node.
	if err := e.be.cs.CheckMember(worker1); err != nil {
		t.Fatal(err)
	}

	// A member may report its own containers...
	c := grpc_public.NewOrchestratorPublicClient(conn)
	if _, err := c.ReportContainers(context.Background(), &grpc_public.ReportContainersRequest{NodeId: worker1, Containers: []*grpc_public.Container{{Id: "c1", DockerId: "d1", NodeId: worker1, Image: "x", Name: "n"}}}); err != nil {
		t.Fatalf("own report: %v", err)
	}
	// ... but not someone else's, and cannot create nodes or list them.
	if _, err := c.ReportContainers(context.Background(), &grpc_public.ReportContainersRequest{NodeId: worker2}); status.Code(err) != codes.PermissionDenied {
		t.Errorf("report for another node: %v, want PermissionDenied", err)
	}
	if _, err := c.ReportContainers(context.Background(), &grpc_public.ReportContainersRequest{NodeId: worker1, Containers: []*grpc_public.Container{{DockerId: "d2", NodeId: worker2}}}); status.Code(err) != codes.PermissionDenied {
		t.Errorf("report smuggling another node's container: %v, want PermissionDenied", err)
	}
	if _, err := c.CreateNode(context.Background(), &grpc_public.CreateNodeRequest{Name: "evil"}); status.Code(err) != codes.Unimplemented {
		t.Errorf("CreateNode on the cluster listener: %v, want Unimplemented", err)
	}
	if _, err := c.GetNodes(context.Background(), &grpc_public.GetNodesRequest{}); status.Code(err) != codes.Unimplemented {
		t.Errorf("GetNodes on the cluster listener: %v, want Unimplemented", err)
	}
	// Renewal for another identity is refused.
	_, otherCSR, _ := pki.GenerateNodeKey(worker2)
	if _, err := grpc_public.NewClusterPublicClient(conn).Renew(context.Background(), &grpc_public.RenewRequest{CsrPem: string(otherCSR)}); status.Code(err) != codes.PermissionDenied {
		t.Errorf("cross-identity renew: %v, want PermissionDenied", err)
	}
}

func TestWrongFingerprintRefusesBeforeTheSecretIsSent(t *testing.T) {
	e := newEnv(t)
	res, err := e.be.cs.AddNode(e.addr)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := pki.ParseJoinString(res.JoinString)
	info.Fingerprint[0] ^= 0xff // a MITM / the wrong cluster

	before := e.srv.JoinAttempts()
	if _, err := pki.Join(context.Background(), info, worker1, pki.JoinNode{}); err == nil {
		t.Fatal("join succeeded with a wrong fingerprint")
	}
	if got := e.srv.JoinAttempts(); got != before {
		t.Fatalf("the join endpoint received %d request(s): the secret was sent although the fingerprint did not match", got-before)
	}
	// The secret is still unspent: the right fingerprint works.
	info.Fingerprint[0] ^= 0xff
	if _, err := pki.Join(context.Background(), info, worker1, pki.JoinNode{}); err != nil {
		t.Fatalf("join with the right fingerprint: %v", err)
	}
}

func TestReusedAndExpiredSecretsFailOverTheWire(t *testing.T) {
	e := newEnv(t)
	res, _ := e.be.cs.AddNode(e.addr)
	info, _ := pki.ParseJoinString(res.JoinString)
	if _, err := pki.Join(context.Background(), info, worker1, pki.JoinNode{}); err != nil {
		t.Fatal(err)
	}
	if _, err := pki.Join(context.Background(), info, worker2, pki.JoinNode{}); err != pki.ErrJoinRejected {
		t.Fatalf("reused secret: err = %v, want ErrJoinRejected", err)
	}

	// Expiry: move the manager's clock past 15 minutes.
	res2, _ := e.be.cs.AddNode(e.addr)
	info2, _ := pki.ParseJoinString(res2.JoinString)
	e.be.cs.Now = func() time.Time { return time.Now().Add(16 * time.Minute) }
	if _, err := pki.Join(context.Background(), info2, worker2, pki.JoinNode{}); err != pki.ErrJoinRejected {
		t.Fatalf("expired secret: err = %v, want ErrJoinRejected", err)
	}
}

func TestNonMTLSClientsAreRefused(t *testing.T) {
	e := newEnv(t)
	res, _ := e.be.cs.AddNode(e.addr)
	m := e.joinNode(t, res.JoinString, worker1)
	pool, _ := pki.CertPool(m.Bundle())

	// (a) gRPC with no client certificate.
	noCert := &tls.Config{RootCAs: pool, ServerName: pki.ServerName, MinVersion: tls.VersionTLS13}
	conn, _ := grpc.NewClient(e.addr, grpc.WithTransportCredentials(credentials.NewTLS(noCert)))
	defer conn.Close()
	if err := ping(context.Background(), conn); err == nil {
		t.Fatal("gRPC without a client certificate was served")
	}

	// (b) gRPC over plaintext.
	plain, _ := grpc.NewClient(e.addr, grpc.WithTransportCredentials(insecureCreds()))
	defer plain.Close()
	if err := ping(context.Background(), plain); err == nil {
		t.Fatal("plaintext gRPC was served")
	}

	// (c) a certificate from a different CA.
	foreign, _ := pki.NewCA("foreign", time.Now())
	key, csrPEM, _ := pki.GenerateNodeKey(worker1)
	csr, _ := pki.ParseCSR(csrPEM)
	nc, _ := foreign.SignNode(csr, time.Now())
	keyPEM, _ := pki.MarshalKeyPEM(key)
	cert, err := tls.X509KeyPair(append(keyPEM, nc.PEM...), append(keyPEM, nc.PEM...))
	if err != nil {
		t.Fatal(err)
	}
	forged := &tls.Config{RootCAs: pool, ServerName: pki.ServerName, MinVersion: tls.VersionTLS13,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return &cert, nil }}
	conn3, _ := grpc.NewClient(e.addr, grpc.WithTransportCredentials(credentials.NewTLS(forged)))
	defer conn3.Close()
	if err := ping(context.Background(), conn3); err == nil {
		t.Fatal("a certificate from a foreign CA was accepted")
	}
}

func TestNodesRmRevokesLiveAndNewConnections(t *testing.T) {
	e := newEnv(t)
	res, _ := e.be.cs.AddNode(e.addr)
	m := e.joinNode(t, res.JoinString, worker1)

	live, _ := clients.DialCluster(m)
	defer live.Close()
	if err := ping(context.Background(), live); err != nil {
		t.Fatal(err)
	}

	n, err := e.be.cs.RemoveNode(worker1)
	if err != nil || n != 1 {
		t.Fatalf("RemoveNode = %d, %v", n, err)
	}

	// The already-open connection is refused on its next RPC...
	err = ping(context.Background(), live)
	if c := status.Code(err); c != codes.Unauthenticated && c != codes.PermissionDenied && c != codes.Unavailable {
		t.Fatalf("revoked member's live connection still works: err = %v", err)
	}
	// ... and a brand-new connection fails at the handshake.
	fresh, _ := clients.DialCluster(m)
	defer fresh.Close()
	if err := ping(context.Background(), fresh); err == nil {
		t.Fatal("revoked member reconnected")
	}
	// Re-enrolling with a fresh string brings the same node id back.
	res2, _ := e.be.cs.AddNode(e.addr)
	m2 := e.joinNode(t, res2.JoinString, worker1)
	again, _ := clients.DialCluster(m2)
	defer again.Close()
	if err := ping(context.Background(), again); err != nil {
		t.Fatalf("re-enrolled node: %v", err)
	}
	// And the old credential stays dead.
	if err := ping(context.Background(), fresh); err == nil {
		t.Fatal("the revoked certificate works again after re-enrollment")
	}
}

func TestRenewalOverTheLiveLink(t *testing.T) {
	e := newEnv(t)
	res, _ := e.be.cs.AddNode(e.addr)
	m := e.joinNode(t, res.JoinString, worker1)
	conn, _ := clients.DialCluster(m)
	defer conn.Close()

	// Not due: nothing happens.
	renewed, err := clients.RenewIfDue(context.Background(), conn, m, time.Now())
	if err != nil || renewed {
		t.Fatalf("early renewal: renewed=%v err=%v", renewed, err)
	}
	oldSerial := m.Serial()

	// 80 days before expiry (inside the 90-day window): renews.
	renewed, err = clients.RenewIfDue(context.Background(), conn, m, m.NotAfter().Add(-80*24*time.Hour))
	if err != nil || !renewed {
		t.Fatalf("renewal in window: renewed=%v err=%v", renewed, err)
	}
	if m.Serial() == oldSerial {
		t.Fatal("certificate did not change")
	}
	// The renewed certificate was persisted and works on a new connection.
	reloaded, err := pki.LoadMember(m.Dir)
	if err != nil || reloaded.Serial() != m.Serial() {
		t.Fatalf("persisted serial mismatch: %v", err)
	}
	conn2, _ := clients.DialCluster(reloaded)
	defer conn2.Close()
	if err := ping(context.Background(), conn2); err != nil {
		t.Fatalf("renewed certificate rejected: %v", err)
	}
}

func TestListenerExistsOnlyAfterNodesAdd(t *testing.T) {
	e := newEnv(t)
	if err := e.be.cs.Start(); err != nil {
		t.Fatal(err)
	}
	if e.srv.Addr() != "" {
		t.Fatal("the cluster listener is up before `nodes add`")
	}
	if c, err := net.DialTimeout("tcp", e.addr, 200*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("something is listening on the cluster port before `nodes add`")
	}
	if _, err := e.be.cs.AddNode(e.addr); err != nil {
		t.Fatal(err)
	}
	if e.srv.Addr() == "" {
		t.Fatal("listener not started by nodes add")
	}
}

func insecureCreds() credentials.TransportCredentials { return insecure.NewCredentials() }
