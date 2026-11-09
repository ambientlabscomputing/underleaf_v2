package pki

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testNodeID = "node_d0abcdefghij0123456k"

func mustCA(t *testing.T) *CA {
	t.Helper()
	ca, err := NewCA("test CA", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

func TestCALifetimeAndPersistence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ca")
	now := time.Now()
	ca, created, err := LoadOrCreateCA(dir, now)
	if err != nil || !created {
		t.Fatalf("create: created=%v err=%v", created, err)
	}
	if got := ca.Cert.NotAfter.Sub(now); got < 10*365*24*time.Hour-time.Hour || got > 10*365*24*time.Hour+time.Hour {
		t.Errorf("CA lifetime = %v, want ~10 years", got)
	}
	if !ca.Cert.IsCA {
		t.Error("CA cert is not a CA")
	}

	// The key must be private to the owner and the dir too.
	if fi, err := os.Stat(filepath.Join(dir, "ca_key.pem")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("ca_key.pem mode = %v err=%v, want 0600", fi.Mode().Perm(), err)
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("CA dir mode = %v, want 0700", fi.Mode().Perm())
	}

	again, created, err := LoadOrCreateCA(dir, now.Add(time.Hour))
	if err != nil || created {
		t.Fatalf("reload: created=%v err=%v", created, err)
	}
	if again.Fingerprint() != ca.Fingerprint() {
		t.Error("reloaded CA has a different fingerprint")
	}
}

func TestCARefusesToRegenerateOverPartialState(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := LoadOrCreateCA(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Losing the key but keeping the cert must not silently mint a new CA
	// (that would orphan every enrolled node).
	if err := os.Remove(filepath.Join(dir, "ca_key.pem")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOrCreateCA(dir, time.Now()); err == nil {
		t.Fatal("expected refusal to regenerate a CA over a partial one")
	}
}

func TestCARejectsMismatchedKey(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	if _, _, err := LoadOrCreateCA(dirA, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOrCreateCA(dirB, time.Now()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dirB, "ca_key.pem"))
	if err := os.WriteFile(filepath.Join(dirA, "ca_key.pem"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOrCreateCA(dirA, time.Now()); err == nil {
		t.Fatal("expected error for key/cert mismatch")
	}
}

func TestSignNodeCert(t *testing.T) {
	ca := mustCA(t)
	_, csrPEM, err := GenerateNodeKey(testNodeID)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := ParseCSR(csrPEM)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	nc, err := ca.SignNode(csr, now)
	if err != nil {
		t.Fatal(err)
	}
	c := nc.Cert
	if c.Subject.CommonName != testNodeID {
		t.Errorf("CN = %q", c.Subject.CommonName)
	}
	if got := c.NotAfter.Sub(now); got < 730*24*time.Hour-time.Minute || got > 730*24*time.Hour+time.Minute {
		t.Errorf("node cert lifetime = %v, want 730 days", got)
	}
	if !c.NotBefore.Before(now) {
		t.Error("NotBefore is not backdated")
	}
	if skew := now.Sub(c.NotBefore); skew < 3*time.Minute || skew > 10*time.Minute {
		t.Errorf("NotBefore backdate = %v, want a few minutes", skew)
	}
	if c.IsCA {
		t.Error("node cert must not be a CA")
	}
	if len(c.ExtKeyUsage) != 1 || c.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Errorf("ExtKeyUsage = %v, want client-auth only (a node must not be able to act as the manager)", c.ExtKeyUsage)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	if _, err := c.Verify(verifyClientOpts(pool)); err != nil {
		t.Errorf("verify against CA: %v", err)
	}
	// ... but not as a server.
	if _, err := c.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err == nil {
		t.Error("node cert verified for server auth")
	}

	// Serials are unique.
	nc2, _ := ca.SignNode(csr, now)
	if nc2.Serial == nc.Serial {
		t.Error("serials collide")
	}
}

func TestSignIgnoresCSRRequestedExtensions(t *testing.T) {
	ca := mustCA(t)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: testNodeID},
		DNSNames: []string{"orchestrator.cluster.underleaf", "evil.example"},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := ParseCSR(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
	if err != nil {
		t.Fatal(err)
	}
	nc, err := ca.SignNode(csr, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(nc.Cert.DNSNames) != 0 || len(nc.Cert.IPAddresses) != 0 {
		t.Errorf("requested SANs leaked into the certificate: %v", nc.Cert.DNSNames)
	}
}

func TestParseCSRRejects(t *testing.T) {
	good, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	mk := func(cn string, k *ecdsa.PrivateKey) []byte {
		der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: cn}}, k)
		if err != nil {
			t.Fatal(err)
		}
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
	}
	for name, in := range map[string][]byte{
		"empty":           nil,
		"garbage":         []byte("not pem"),
		"wrong block":     pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{1, 2, 3}}),
		"cn not node id":  mk("alice", good),
		"cn is the CA's":  mk("Underleaf cluster CA", good),
		"cn path attempt": mk("node_../../etc", good),
	} {
		if _, err := ParseCSR(in); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	// A CSR whose signature was tampered with is refused.
	pemBytes := mk(testNodeID, good)
	blk, _ := pem.Decode(pemBytes)
	blk.Bytes[len(blk.Bytes)-3] ^= 0xff
	if _, err := ParseCSR(pem.EncodeToMemory(blk)); err == nil {
		t.Error("tampered CSR accepted")
	}
}

func TestTrustBundleHoldsMoreThanOneCA(t *testing.T) {
	dir := t.TempDir()
	a, _, err := LoadOrCreateCA(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	b := mustCA(t)
	// Append a second CA (what a future rotation would do).
	f, _ := os.OpenFile(filepath.Join(dir, "trust_bundle.pem"), os.O_APPEND|os.O_WRONLY, 0o644)
	f.Write(b.CertPEM)
	f.Close()

	bundle, err := TrustBundle(dir, a)
	if err != nil {
		t.Fatal(err)
	}
	certs, err := ParseCertificates(bundle)
	if err != nil || len(certs) != 2 {
		t.Fatalf("bundle has %d certs err=%v, want 2", len(certs), err)
	}
	pool, _ := CertPool(bundle)

	for name, ca := range map[string]*CA{"old": a, "new": b} {
		key, csrPEM, _ := GenerateNodeKey(testNodeID)
		_ = key
		csr, _ := ParseCSR(csrPEM)
		nc, _ := ca.SignNode(csr, time.Now())
		if _, err := nc.Cert.Verify(verifyClientOpts(pool)); err != nil {
			t.Errorf("cert from %s CA does not verify against the bundle: %v", name, err)
		}
	}
	// A CA outside the bundle is not trusted.
	other := mustCA(t)
	_, csrPEM, _ := GenerateNodeKey(testNodeID)
	csr, _ := ParseCSR(csrPEM)
	nc, _ := other.SignNode(csr, time.Now())
	if _, err := nc.Cert.Verify(verifyClientOpts(pool)); err == nil {
		t.Error("cert from an unrelated CA verified")
	}
}

func TestJoinStringRoundTripAndRejects(t *testing.T) {
	secret, _ := NewJoinSecret()
	var fp [32]byte
	rand.Read(fp[:])
	in := JoinInfo{Addr: "10.0.0.5:50104", Fingerprint: fp, Secret: secret, ExpiresAt: time.Now().Add(JoinTTL).Truncate(time.Second)}
	s, err := in.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s, "ulj_") {
		t.Fatalf("join string %q lacks ulj_ prefix", s)
	}
	if strings.ContainsAny(s, " \n+/=") {
		t.Errorf("join string %q is not a single paste-safe token", s)
	}
	out, err := ParseJoinString("  " + s + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if out.Addr != in.Addr || out.Fingerprint != in.Fingerprint || string(out.Secret) != string(in.Secret) || !out.ExpiresAt.Equal(in.ExpiresAt) {
		t.Errorf("round trip mismatch: %+v vs %+v", out, in)
	}
	for name, bad := range map[string]string{
		"no prefix":  strings.TrimPrefix(s, "ulj_"),
		"truncated":  s[:len(s)-60],
		"bad base64": "ulj_!!!!",
		"empty":      "ulj_",
		"wrong ver":  "ulj_" + base64.RawURLEncoding.EncodeToString(append([]byte{9}, make([]byte, 100)...)),
	} {
		if _, err := ParseJoinString(bad); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestSecretHashIsNotTheSecret(t *testing.T) {
	secret, _ := NewJoinSecret()
	h := HashSecret(secret)
	if strings.Contains(h, base64.RawURLEncoding.EncodeToString(secret)) || len(h) != 64 {
		t.Errorf("hash %q looks wrong", h)
	}
	if HashSecret(secret) != h {
		t.Error("hash not deterministic")
	}
}

// joinTestServer serves the join side of the cluster listener with a handler
// that records whether it ever ran.
func joinTestServer(t *testing.T, ca *CA, hits *atomic.Int32, handler http.HandlerFunc) (addr string) {
	t.Helper()
	src := NewServerCertSource(ca, []string{"127.0.0.1"}, nil)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		handler(w, r)
	}), ErrorLog: log.New(io.Discard, "", 0)}
	go srv.Serve(tls.NewListener(lis, JoinServerTLS(src)))
	t.Cleanup(func() { srv.Close() })
	return lis.Addr().String()
}

func TestJoinWrongFingerprintNeverSendsSecret(t *testing.T) {
	realCA := mustCA(t)
	var hits atomic.Int32
	var sawSecret atomic.Bool
	addr := joinTestServer(t, realCA, &hits, func(w http.ResponseWriter, r *http.Request) {
		var req JoinRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Secret != "" {
			sawSecret.Store(true)
		}
		w.WriteHeader(http.StatusUnauthorized)
	})

	secret, _ := NewJoinSecret()
	attackerFP := Fingerprint(mustCA(t).Cert) // a different cluster / a MITM's CA
	_, err := Join(context.Background(), JoinInfo{Addr: addr, Fingerprint: attackerFP, Secret: secret}, testNodeID, JoinNode{})
	if err == nil {
		t.Fatal("join succeeded against a server whose CA does not match the pinned fingerprint")
	}
	if hits.Load() != 0 || sawSecret.Load() {
		t.Fatalf("server saw %d request(s) (secret seen: %v); the secret must not be sent when the fingerprint does not match", hits.Load(), sawSecret.Load())
	}
	if !strings.Contains(err.Error(), "pinned CA") {
		t.Errorf("error does not explain the pinning failure: %v", err)
	}
}

func TestJoinRefusesServerCertNotSignedByPinnedCA(t *testing.T) {
	// A MITM that *copies* the real CA certificate into its chain (so the
	// fingerprint appears) but whose leaf is signed by its own key must fail.
	realCA := mustCA(t)
	mitmCA := mustCA(t)
	leafPair, err := mitmCA.issueServer([]string{"127.0.0.1"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	leafPair.caDER = realCA.Cert.Raw // claim to be under the real CA
	var hits atomic.Int32
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }), ErrorLog: log.New(io.Discard, "", 0)}
	go srv.Serve(tls.NewListener(lis, &tls.Config{Certificates: []tls.Certificate{*leafPair.certificate()}, NextProtos: []string{"http/1.1"}}))
	defer srv.Close()

	secret, _ := NewJoinSecret()
	_, err = Join(context.Background(), JoinInfo{Addr: lis.Addr().String(), Fingerprint: realCA.Fingerprint(), Secret: secret}, testNodeID, JoinNode{})
	if err == nil {
		t.Fatal("join accepted a leaf not signed by the pinned CA")
	}
	if hits.Load() != 0 {
		t.Fatal("request reached the impostor")
	}
}

func TestJoinSuccessAndIssuedCertChecks(t *testing.T) {
	ca := mustCA(t)
	var hits atomic.Int32
	addr := joinTestServer(t, ca, &hits, func(w http.ResponseWriter, r *http.Request) {
		var req JoinRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		csr, err := ParseCSR([]byte(req.CSRPEM))
		if err != nil {
			t.Error(err)
			http.Error(w, "bad csr", 400)
			return
		}
		nc, _ := ca.SignNode(csr, time.Now())
		json.NewEncoder(w).Encode(JoinResponse{NodeID: csr.Subject.CommonName, CertPEM: string(nc.PEM), CABundlePEM: string(ca.CertPEM), NotAfter: nc.NotAfter})
	})
	secret, _ := NewJoinSecret()
	res, err := Join(context.Background(), JoinInfo{Addr: addr, Fingerprint: ca.Fingerprint(), Secret: secret}, testNodeID, JoinNode{Name: "w1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.NodeID != testNodeID || hits.Load() != 1 {
		t.Fatalf("res=%+v hits=%d", res, hits.Load())
	}

	// Persist and reload as a Member, then use it for an mTLS handshake.
	dir := filepath.Join(t.TempDir(), "node")
	if _, err := SaveMember(dir, MemberState{ManagerAddr: addr, NodeID: testNodeID, CAFingerprint: FormatFingerprint(ca.Fingerprint())}, res.Key, res.CertPEM, res.Bundle); err != nil {
		t.Fatal(err)
	}
	m, err := LoadMember(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "node.pem")); fi.Mode().Perm() != 0o600 {
		t.Errorf("node.pem mode = %v, want 0600", fi.Mode().Perm())
	}
	if m.NotAfter().Before(time.Now().Add(729 * 24 * time.Hour)) {
		t.Errorf("NotAfter = %v", m.NotAfter())
	}
}

func TestJoinRejectedMapsToHelpfulError(t *testing.T) {
	ca := mustCA(t)
	var hits atomic.Int32
	addr := joinTestServer(t, ca, &hits, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	secret, _ := NewJoinSecret()
	_, err := Join(context.Background(), JoinInfo{Addr: addr, Fingerprint: ca.Fingerprint(), Secret: secret}, testNodeID, JoinNode{})
	if err != ErrJoinRejected {
		t.Fatalf("err = %v, want ErrJoinRejected", err)
	}
}

func TestJoinRejectsBadIssuedCertificates(t *testing.T) {
	ca := mustCA(t)
	other := mustCA(t)
	otherNode := "node_d1abcdefghij0123456k"
	respond := func(f func(csr *x509.CertificateRequest) JoinResponse) string {
		var hits atomic.Int32
		addr := joinTestServer(t, ca, &hits, func(w http.ResponseWriter, r *http.Request) {
			var req JoinRequest
			json.NewDecoder(r.Body).Decode(&req)
			csr, _ := ParseCSR([]byte(req.CSRPEM))
			json.NewEncoder(w).Encode(f(csr))
		})
		return addr
	}
	cases := map[string]func(csr *x509.CertificateRequest) JoinResponse{
		"cert for another node id": func(csr *x509.CertificateRequest) JoinResponse {
			k, c, _ := GenerateNodeKey(otherNode)
			_ = k
			cc, _ := ParseCSR(c)
			nc, _ := ca.SignNode(cc, time.Now())
			return JoinResponse{NodeID: testNodeID, CertPEM: string(nc.PEM), CABundlePEM: string(ca.CertPEM)}
		},
		"cert for a different key": func(csr *x509.CertificateRequest) JoinResponse {
			_, c, _ := GenerateNodeKey(testNodeID)
			cc, _ := ParseCSR(c)
			nc, _ := ca.SignNode(cc, time.Now())
			return JoinResponse{NodeID: testNodeID, CertPEM: string(nc.PEM), CABundlePEM: string(ca.CertPEM)}
		},
		"bundle without the pinned CA": func(csr *x509.CertificateRequest) JoinResponse {
			nc, _ := ca.SignNode(csr, time.Now())
			return JoinResponse{NodeID: testNodeID, CertPEM: string(nc.PEM), CABundlePEM: string(other.CertPEM)}
		},
		"cert not signed by the bundle": func(csr *x509.CertificateRequest) JoinResponse {
			nc, _ := other.SignNode(csr, time.Now())
			return JoinResponse{NodeID: testNodeID, CertPEM: string(nc.PEM), CABundlePEM: string(ca.CertPEM)}
		},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			addr := respond(f)
			secret, _ := NewJoinSecret()
			if _, err := Join(context.Background(), JoinInfo{Addr: addr, Fingerprint: ca.Fingerprint(), Secret: secret}, testNodeID, JoinNode{}); err == nil {
				t.Fatal("join accepted a bad issuance")
			}
		})
	}
}

func TestNotMember(t *testing.T) {
	if _, err := LoadMember(t.TempDir()); err != ErrNotMember {
		t.Fatalf("err = %v, want ErrNotMember", err)
	}
}

func TestServerCertSourceRenewsBeforeExpiry(t *testing.T) {
	ca := mustCA(t)
	now := time.Now()
	clock := now
	src := NewServerCertSource(ca, []string{"10.1.2.3", "mgr.example"}, func() time.Time { return clock })
	c1, err := src.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	leaf := c1.Leaf
	if leaf.Subject.CommonName != ServerName || len(leaf.IPAddresses) != 1 || !leaf.IPAddresses[0].Equal(net.ParseIP("10.1.2.3")) {
		t.Errorf("unexpected SANs: cn=%s ips=%v dns=%v", leaf.Subject.CommonName, leaf.IPAddresses, leaf.DNSNames)
	}
	c2, _ := src.GetCertificate(nil)
	if c1 != c2 {
		t.Error("certificate re-issued needlessly")
	}
	clock = now.Add(ServerCertLifetime - 10*24*time.Hour) // inside the 30-day window
	c3, _ := src.GetCertificate(nil)
	if c3 == c1 {
		t.Error("certificate was not re-issued inside the renewal window")
	}
}
