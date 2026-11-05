package service

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	_ "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository/migrations"
	"github.com/ambientlabscomputing/underleaf_v2/shared/pki"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

const (
	nodeX = "node_d0abcdefghij0123456k"
	nodeY = "node_d1abcdefghij0123456k"
)

type clusterEnv struct {
	svc     *ClusterService
	repo    *repository.Repository
	dbPath  string
	clock   time.Time
	started []string
	removed []string
}

func newClusterEnv(t *testing.T) *clusterEnv {
	t.Helper()
	dir := t.TempDir()
	env := &clusterEnv{dbPath: filepath.Join(dir, "orch.db"), clock: time.Now()}
	repo, err := repository.NewRepositoryAt(env.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	env.repo = repo
	env.svc = NewClusterService(repo, filepath.Join(dir, "ca"), "50104")
	env.svc.Now = func() time.Time { return env.clock }
	env.svc.PrimaryIP = func() (string, error) { return "192.0.2.10", nil }
	env.svc.StartListener = func(adv string) error { env.started = append(env.started, adv); return nil }
	env.svc.OnNodeRemoved = func(id string) { env.removed = append(env.removed, id) }
	if err := env.svc.Start(); err != nil {
		t.Fatal(err)
	}
	return env
}

// joinReq builds a join request for nodeID using the secret in a join string.
func joinReq(t *testing.T, joinString, nodeID string) pki.JoinRequest {
	t.Helper()
	info, err := pki.ParseJoinString(joinString)
	if err != nil {
		t.Fatal(err)
	}
	_, csr, err := pki.GenerateNodeKey(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	return pki.JoinRequest{
		Secret: base64.RawURLEncoding.EncodeToString(info.Secret),
		CSRPEM: string(csr),
		Node:   pki.JoinNode{Name: "worker-1", IPAddr: "10.0.0.7", OS: "linux", Arch: "arm64"},
	}
}

func (e *clusterEnv) addNode(t *testing.T) string {
	t.Helper()
	res, err := e.svc.AddNode("")
	if err != nil {
		t.Fatal(err)
	}
	return res.JoinString
}

func TestAddNodeEnablesListenerAndStoresOnlyAHash(t *testing.T) {
	e := newClusterEnv(t)
	if e.svc.ListenerEnabled() {
		t.Fatal("listener must not be enabled before `nodes add`")
	}
	res, err := e.svc.AddNode("")
	if err != nil {
		t.Fatal(err)
	}
	if !e.svc.ListenerEnabled() || len(e.started) != 1 || e.started[0] != "192.0.2.10:50104" {
		t.Fatalf("enabled=%v started=%v", e.svc.ListenerEnabled(), e.started)
	}
	info, err := pki.ParseJoinString(res.JoinString)
	if err != nil {
		t.Fatal(err)
	}
	fp, _ := e.svc.Fingerprint()
	if info.Fingerprint != fp || info.Addr != "192.0.2.10:50104" {
		t.Errorf("join string carries %s %s", info.Addr, pki.FormatFingerprint(info.Fingerprint))
	}
	if d := info.ExpiresAt.Sub(e.clock); d < 14*time.Minute || d > 16*time.Minute {
		t.Errorf("join string lifetime = %v, want 15 minutes", d)
	}

	// The database contains the hash and not the secret, in any encoding.
	rows, _ := e.repo.Cluster.ListJoinTokens()
	if len(rows) != 1 || rows[0].SecretHash != pki.HashSecret(info.Secret) {
		t.Fatalf("token rows = %+v", rows)
	}
	b64 := base64.RawURLEncoding.EncodeToString(info.Secret)
	for _, f := range []string{e.dbPath, e.dbPath + "-wal"} {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if bytes.Contains(raw, info.Secret) || bytes.Contains(raw, []byte(b64)) || bytes.Contains(raw, []byte(res.JoinString)) {
			t.Errorf("%s contains the plaintext join secret", f)
		}
	}
}

func TestAdvertiseResolution(t *testing.T) {
	e := newClusterEnv(t)
	for in, want := range map[string]string{
		"mgr.example.com":      "mgr.example.com:50104",
		"10.1.1.1:6000":        "10.1.1.1:6000",
		"[2001:db8::1]:50104":  "[2001:db8::1]:50104",
		"2001:db8::1":          "[2001:db8::1]:50104",
		"  mgr.example.com  ":  "mgr.example.com:50104",
		"host.docker.internal": "host.docker.internal:50104",
	} {
		got, err := e.svc.resolveAdvertise(in)
		if err != nil || got != want {
			t.Errorf("resolveAdvertise(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"0.0.0.0", "0.0.0.0:50104", ":50104", "[::]:1", "224.0.0.1"} {
		if _, err := e.svc.resolveAdvertise(bad); err == nil {
			t.Errorf("resolveAdvertise(%q) should fail", bad)
		}
	}
}

func TestJoinHappyPath(t *testing.T) {
	e := newClusterEnv(t)
	resp, err := e.svc.Join(joinReq(t, e.addNode(t), nodeX))
	if err != nil {
		t.Fatal(err)
	}
	if resp.NodeID != nodeX {
		t.Fatalf("node id %q", resp.NodeID)
	}
	if d := resp.NotAfter.Sub(e.clock); d < 729*24*time.Hour {
		t.Errorf("cert lifetime %v, want 730 days", d)
	}
	n, err := e.repo.Nodes.GetNodeByID(nodeX)
	if err != nil || n.Transport != types.TransportCluster || n.Name != "worker-1" || n.Arch != "arm64" {
		t.Fatalf("node row = %+v err=%v", n, err)
	}
	certs, _ := e.repo.Cluster.ListNodeCerts(nodeX)
	if len(certs) != 1 {
		t.Fatalf("recorded certs = %d", len(certs))
	}
	if !strings.Contains(resp.CABundlePEM, "BEGIN CERTIFICATE") {
		t.Error("no CA bundle returned")
	}
}

func TestJoinSecretIsSingleUse(t *testing.T) {
	e := newClusterEnv(t)
	js := e.addNode(t)
	if _, err := e.svc.Join(joinReq(t, js, nodeX)); err != nil {
		t.Fatal(err)
	}
	// A different node id with the same (now spent) secret.
	_, err := e.svc.Join(joinReq(t, js, nodeY))
	if !errors.Is(err, ErrJoinDenied) {
		t.Fatalf("reused secret: err = %v, want ErrJoinDenied", err)
	}
	if _, err := e.repo.Nodes.GetNodeByID(nodeY); err == nil {
		t.Error("a node was created from a reused secret")
	}
	// Same node id again is also refused (reuse is about the secret).
	if _, err := e.svc.Join(joinReq(t, js, nodeX)); !errors.Is(err, ErrJoinDenied) {
		t.Fatalf("reused secret (same node): err = %v", err)
	}
}

func TestJoinSecretExpires(t *testing.T) {
	e := newClusterEnv(t)
	js := e.addNode(t)
	e.clock = e.clock.Add(pki.JoinTTL + time.Second)
	if _, err := e.svc.Join(joinReq(t, js, nodeX)); !errors.Is(err, ErrJoinDenied) {
		t.Fatalf("expired secret: err = %v, want ErrJoinDenied", err)
	}
	if _, err := e.repo.Nodes.GetNodeByID(nodeX); err == nil {
		t.Error("a node was created from an expired secret")
	}
}

func TestJoinSecretAtExactExpiryBoundaryIsRefused(t *testing.T) {
	e := newClusterEnv(t)
	js := e.addNode(t)
	e.clock = e.clock.Add(pki.JoinTTL)
	if _, err := e.svc.Join(joinReq(t, js, nodeX)); !errors.Is(err, ErrJoinDenied) {
		t.Fatalf("secret at expiry instant: err = %v, want ErrJoinDenied", err)
	}
}

func TestJoinWrongOrMalformedSecretIsIndistinguishable(t *testing.T) {
	e := newClusterEnv(t)
	js := e.addNode(t)
	good := joinReq(t, js, nodeX)

	wrong := good
	other := bytes.Repeat([]byte{7}, 32)
	wrong.Secret = base64.RawURLEncoding.EncodeToString(other)
	short := good
	short.Secret = "AAAA"
	garbage := good
	garbage.Secret = "!!!not base64!!!"
	empty := good
	empty.Secret = ""
	for name, req := range map[string]pki.JoinRequest{"wrong": wrong, "short": short, "garbage": garbage, "empty": empty} {
		if _, err := e.svc.Join(req); !errors.Is(err, ErrJoinDenied) {
			t.Errorf("%s secret: err = %v, want ErrJoinDenied", name, err)
		}
	}
	// ... and the real secret still works: failed attempts did not burn it.
	if _, err := e.svc.Join(good); err != nil {
		t.Fatalf("valid join after failed attempts: %v", err)
	}
}

func TestJoinBadCSRDoesNotBurnTheSecretAndLeaksNothingWithoutIt(t *testing.T) {
	e := newClusterEnv(t)
	js := e.addNode(t)
	req := joinReq(t, js, nodeX)

	bad := req
	bad.CSRPEM = "garbage"
	if _, err := e.svc.Join(bad); !errors.Is(err, ErrBadJoin) {
		t.Fatalf("bad CSR with valid secret: err = %v, want ErrBadJoin", err)
	}
	// Without a valid secret the same bad CSR is just "denied" (no oracle).
	noSecret := bad
	noSecret.Secret = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	if _, err := e.svc.Join(noSecret); !errors.Is(err, ErrJoinDenied) {
		t.Fatalf("bad CSR with bad secret: err = %v, want ErrJoinDenied", err)
	}
	// A well-formed, correctly signed CSR whose CN is not a node id (e.g. an
	// attempt to get a certificate named like the manager) is refused too.
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: pki.ServerName}}, key)
	cnBad := req
	cnBad.CSRPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
	if _, err := e.svc.Join(cnBad); !errors.Is(err, ErrBadJoin) {
		t.Fatalf("CSR with a non-node CN: err = %v, want ErrBadJoin", err)
	}
	if _, err := e.svc.Join(req); err != nil {
		t.Fatalf("secret was burned by the earlier bad CSR: %v", err)
	}
}

func TestReEnrollKeepsNodeIDAndRevokesOldCertificate(t *testing.T) {
	e := newClusterEnv(t)
	if _, err := e.svc.Join(joinReq(t, e.addNode(t), nodeX)); err != nil {
		t.Fatal(err)
	}
	old, _ := e.repo.Cluster.ListNodeCerts(nodeX)
	if len(old) != 1 {
		t.Fatal("setup")
	}
	if _, err := e.svc.Join(joinReq(t, e.addNode(t), nodeX)); err != nil {
		t.Fatalf("re-enroll with a fresh string: %v", err)
	}
	nodes, total, _ := e.repo.Nodes.ListNodes(types.QueryNodesRequest{})
	if total != 1 || len(nodes) != 1 || nodes[0].ID != nodeX {
		t.Fatalf("re-enroll must keep exactly one node with the same id, got %+v", nodes)
	}
	if !e.svc.IsRevoked(old[0].Serial) {
		t.Error("the superseded certificate is not on the deny list")
	}
	cur, _ := e.repo.Cluster.ListNodeCerts(nodeX)
	if len(cur) != 1 || cur[0].Serial == old[0].Serial || e.svc.IsRevoked(cur[0].Serial) {
		t.Errorf("current certificates = %+v", cur)
	}
}

func TestJoinRefusesTheLocalNodesID(t *testing.T) {
	e := newClusterEnv(t)
	if err := e.repo.Nodes.CreateNode(&types.Node{ID: nodeX, Name: "manager-agent"}); err != nil {
		t.Fatal(err)
	}
	js := e.addNode(t)
	if _, err := e.svc.Join(joinReq(t, js, nodeX)); !errors.Is(err, ErrBadJoin) {
		t.Fatalf("err = %v, want ErrBadJoin", err)
	}
	n, _ := e.repo.Nodes.GetNodeByID(nodeX)
	if n.Transport != types.TransportLocal {
		t.Error("a join converted the local node to a cluster node")
	}
	// The refusal did not burn the secret.
	if _, err := e.svc.Join(joinReq(t, js, nodeY)); err != nil {
		t.Fatalf("secret was burned: %v", err)
	}
}

func TestRenew(t *testing.T) {
	e := newClusterEnv(t)
	if _, err := e.svc.Join(joinReq(t, e.addNode(t), nodeX)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Join(joinReq(t, e.addNode(t), nodeY)); err != nil {
		t.Fatal(err)
	}
	e.clock = e.clock.Add(700 * 24 * time.Hour)
	_, csrX, _ := pki.GenerateNodeKey(nodeX)

	// A node cannot renew someone else's identity.
	if _, err := e.svc.Renew(nodeY, csrX); !errors.Is(err, ErrNodeForbidden) {
		t.Fatalf("cross-node renewal: err = %v, want ErrNodeForbidden", err)
	}
	// A caller that is not a cluster member (e.g. removed) cannot renew.
	_, csrZ, _ := pki.GenerateNodeKey("node_d2abcdefghij0123456k")
	if _, err := e.svc.Renew("node_d2abcdefghij0123456k", csrZ); !errors.Is(err, ErrNodeForbidden) {
		t.Fatalf("non-member renewal: err = %v, want ErrNodeForbidden", err)
	}
	res, err := e.svc.Renew(nodeX, csrX)
	if err != nil {
		t.Fatal(err)
	}
	if d := res.NotAfter.Sub(e.clock); d < 729*24*time.Hour {
		t.Errorf("renewed lifetime = %v", d)
	}
	certs, _ := e.repo.Cluster.ListNodeCerts(nodeX)
	if len(certs) != 2 {
		t.Fatalf("expected the old and the renewed certificate to be recorded, got %d", len(certs))
	}
	for _, c := range certs {
		if e.svc.IsRevoked(c.Serial) {
			t.Error("renewal must not revoke the previous certificate (a node that crashed before saving the new one would be locked out)")
		}
	}
	// A garbage CSR is refused.
	if _, err := e.svc.Renew(nodeX, []byte("nope")); !errors.Is(err, ErrBadJoin) {
		t.Fatalf("garbage CSR: err = %v", err)
	}
}

func TestRemoveNodeRevokesEveryCertificate(t *testing.T) {
	e := newClusterEnv(t)
	if _, err := e.svc.Join(joinReq(t, e.addNode(t), nodeX)); err != nil {
		t.Fatal(err)
	}
	_, csr, _ := pki.GenerateNodeKey(nodeX)
	if _, err := e.svc.Renew(nodeX, csr); err != nil { // two live certificates
		t.Fatal(err)
	}
	certs, _ := e.repo.Cluster.ListNodeCerts(nodeX)
	if len(certs) != 2 {
		t.Fatal("setup")
	}
	n, err := e.svc.RemoveNode(nodeX)
	if err != nil || n != 2 {
		t.Fatalf("RemoveNode = %d, %v", n, err)
	}
	for _, c := range certs {
		if !e.svc.IsRevoked(c.Serial) {
			t.Errorf("serial %s still valid after nodes rm", c.Serial)
		}
	}
	if _, err := e.repo.Nodes.GetNodeByID(nodeX); err == nil {
		t.Error("node row still present")
	}
	if len(e.removed) != 1 || e.removed[0] != nodeX {
		t.Errorf("OnNodeRemoved calls = %v", e.removed)
	}
	// The deny list survives a restart of the service.
	svc2 := NewClusterService(e.repo, e.svc.caDir, "50104")
	svc2.Now = e.svc.Now
	if err := svc2.Start(); err != nil {
		t.Fatal(err)
	}
	if !svc2.IsRevoked(certs[0].Serial) {
		t.Error("deny list was not restored from the database")
	}
	// And a removed node cannot renew.
	if _, err := e.svc.Renew(nodeX, csr); !errors.Is(err, ErrNodeForbidden) {
		t.Errorf("removed node renewal: err = %v", err)
	}
	if _, err := e.svc.RemoveNode("node_dnonexistent000000000"); err == nil {
		t.Error("removing an unknown node should fail")
	}
}

func TestRemoveLocalNode(t *testing.T) {
	e := newClusterEnv(t)
	if err := e.repo.Nodes.CreateNode(&types.Node{ID: nodeX, Name: "manager-agent"}); err != nil {
		t.Fatal(err)
	}
	n, err := e.svc.RemoveNode(nodeX)
	if err != nil || n != 0 {
		t.Fatalf("RemoveNode(local) = %d, %v", n, err)
	}
}

func TestDenyListIsPrunedAfterCertificateExpiry(t *testing.T) {
	e := newClusterEnv(t)
	if _, err := e.svc.Join(joinReq(t, e.addNode(t), nodeX)); err != nil {
		t.Fatal(err)
	}
	certs, _ := e.repo.Cluster.ListNodeCerts(nodeX)
	if _, err := e.svc.RemoveNode(nodeX); err != nil {
		t.Fatal(err)
	}
	if n, _ := e.repo.Cluster.CountRevoked(); n != 1 {
		t.Fatalf("deny list rows = %d", n)
	}
	if n, _ := e.svc.PruneRevoked(); n != 0 {
		t.Fatalf("pruned %d entries before expiry", n)
	}
	if !e.svc.IsRevoked(certs[0].Serial) {
		t.Fatal("entry lost before the certificate expired")
	}
	e.clock = certs[0].NotAfter.Add(time.Hour)
	if n, err := e.svc.PruneRevoked(); err != nil || n != 1 {
		t.Fatalf("pruned %d, %v after expiry", n, err)
	}
	if e.svc.IsRevoked(certs[0].Serial) {
		t.Error("expired entry is still on the in-memory deny list")
	}
}

func TestListNodesShowsExpiryAndWarnsAt30Days(t *testing.T) {
	e := newClusterEnv(t)
	if err := e.repo.Nodes.CreateNode(&types.Node{ID: nodeY, Name: "manager-agent"}); err != nil {
		t.Fatal(err)
	}
	resp, err := e.svc.Join(joinReq(t, e.addNode(t), nodeX))
	if err != nil {
		t.Fatal(err)
	}
	find := func() map[string]NodeInfo {
		list, err := e.svc.ListNodes()
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]NodeInfo{}
		for _, i := range list {
			m[i.Node.ID] = i
		}
		return m
	}
	m := find()
	if !m[nodeX].NotAfter.Equal(resp.NotAfter.Truncate(time.Second)) || m[nodeX].Warn {
		t.Fatalf("fresh node info = %+v", m[nodeX])
	}
	if !m[nodeY].NotAfter.IsZero() || m[nodeY].Warn {
		t.Errorf("local node should have no certificate state: %+v", m[nodeY])
	}
	e.clock = resp.NotAfter.Add(-31 * 24 * time.Hour)
	if find()[nodeX].Warn {
		t.Error("warned at 31 days")
	}
	e.clock = resp.NotAfter.Add(-29 * 24 * time.Hour)
	if !find()[nodeX].Warn {
		t.Error("did not warn at 29 days")
	}
	e.clock = resp.NotAfter.Add(time.Hour)
	if got := find()[nodeX]; !got.Warn || !got.Expired {
		t.Errorf("expired cert: %+v", got)
	}
}

func TestServerTLSRefusesRevokedAndForeignCerts(t *testing.T) {
	e := newClusterEnv(t)
	mtls, join, err := e.svc.ServerTLS("192.0.2.10:50104")
	if err != nil {
		t.Fatal(err)
	}
	if mtls.ClientAuth == 0 || join.ClientAuth != 0 {
		t.Fatalf("ClientAuth mtls=%v join=%v", mtls.ClientAuth, join.ClientAuth)
	}
}
