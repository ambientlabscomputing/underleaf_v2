package clients

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

type testCert struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	der  []byte
}

func (c testCert) pem() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.der})
}

func (c testCert) tls() tls.Certificate {
	return tls.Certificate{Certificate: [][]byte{c.der}, PrivateKey: c.key}
}

func issue(t *testing.T, tmpl *x509.Certificate, parent *testCert) testCert {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl.SerialNumber, _ = rand.Int(rand.Reader, big.NewInt(1<<62))
	if tmpl.NotBefore.IsZero() {
		tmpl.NotBefore = time.Now().Add(-time.Hour)
		tmpl.NotAfter = time.Now().Add(time.Hour)
	}
	signer, signerKey := tmpl, key
	if parent != nil {
		signer, signerKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, signer, &key.PublicKey, signerKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return testCert{cert: cert, key: key, der: der}
}

func newCA(t *testing.T, cn string) testCert {
	return issue(t, &x509.Certificate{
		Subject: pkix.Name{CommonName: cn}, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}, nil)
}

func serve(t *testing.T, c testCert, h http.Handler) *httptest.Server {
	t.Helper()
	s := httptest.NewUnstartedServer(h)
	s.TLS = &tls.Config{Certificates: []tls.Certificate{c.tls()}}
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}

func pinnedClient(t *testing.T, ca testCert) *http.Client {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca_chain.pem")
	if err := os.WriteFile(path, ca.pem(), 0o600); err != nil {
		t.Fatal(err)
	}
	hc, err := NewPinnedHTTPClient(path, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return hc
}

var ok = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("hi")) })

func get(hc *http.Client, url string) error {
	resp, err := hc.Get(url)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Today's cloud: nginx presents the CA certificate itself, which has no SAN.
// Standard verification fails; the pinned client accepts it.
func TestPinned_CAPresentedAsServerCertNoSAN(t *testing.T) {
	ca := newCA(t, "Underleaf OAuth Root CA")
	srv := serve(t, ca, ok)

	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	std := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	if err := get(std, srv.URL); err == nil || !strings.Contains(err.Error(), "doesn't contain any IP SANs") {
		t.Fatalf("expected the standard client to fail on missing SANs, got %v", err)
	}
	if err := get(pinnedClient(t, ca), srv.URL); err != nil {
		t.Fatalf("pinned client should accept the pinned CA as server cert: %v", err)
	}
}

func TestPinned_RejectsEverythingNotChainingToThePinnedCA(t *testing.T) {
	ca, other := newCA(t, "pinned"), newCA(t, "other")
	// A certificate from an unrelated CA, even with a matching SAN.
	leaf := issue(t, &x509.Certificate{
		Subject: pkix.Name{CommonName: "x"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, &other)
	if err := get(pinnedClient(t, ca), serve(t, leaf, ok).URL); err == nil {
		t.Fatal("certificate from another CA was accepted")
	}
	// A self-signed impostor with the same subject as the pinned CA.
	imp := newCA(t, "pinned")
	if err := get(pinnedClient(t, ca), serve(t, imp, ok).URL); err == nil {
		t.Fatal("impostor CA with the same name was accepted")
	}
}

func TestPinned_CAIssuedLeafIsHostVerified(t *testing.T) {
	ca := newCA(t, "pinned")
	good := issue(t, &x509.Certificate{
		Subject: pkix.Name{CommonName: "cloud"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, &ca)
	if err := get(pinnedClient(t, ca), serve(t, good, ok).URL); err != nil {
		t.Fatalf("CA-issued leaf for the dialed host rejected: %v", err)
	}
	// Has a SAN, but for a different name: host verification must now apply.
	wrong := issue(t, &x509.Certificate{
		Subject: pkix.Name{CommonName: "cloud"}, DNSNames: []string{"cloud.example"},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, &ca)
	if err := get(pinnedClient(t, ca), serve(t, wrong, ok).URL); err == nil {
		t.Fatal("leaf with SAN for another host was accepted")
	}
}

func TestPinned_ClientAuthCertCannotPoseAsServer(t *testing.T) {
	ca := newCA(t, "pinned")
	clientCert := issue(t, &x509.Certificate{
		Subject: pkix.Name{CommonName: "cluster_x"}, DNSNames: []string{"cluster_x"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}, &ca)
	if err := get(pinnedClient(t, ca), serve(t, clientCert, ok).URL); err == nil {
		t.Fatal("a clientAuth-only certificate was accepted as a server certificate")
	}
}

func TestPinned_ExpiredServerCertRejected(t *testing.T) {
	ca := newCA(t, "pinned")
	expired := issue(t, &x509.Certificate{
		Subject: pkix.Name{CommonName: "cloud"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		NotBefore:   time.Now().Add(-2 * time.Hour), NotAfter: time.Now().Add(-time.Hour),
	}, &ca)
	if err := get(pinnedClient(t, ca), serve(t, expired, ok).URL); err == nil {
		t.Fatal("expired certificate accepted")
	}
}

func TestNewPinnedHTTPClientErrors(t *testing.T) {
	if _, err := NewPinnedHTTPClient(filepath.Join(t.TempDir(), "missing.pem"), time.Second); err == nil {
		t.Fatal("missing CA file must be an error (unregistered cluster fails closed)")
	}
	empty := filepath.Join(t.TempDir(), "x.pem")
	_ = os.WriteFile(empty, []byte("not pem"), 0o600)
	if _, err := NewPinnedHTTPClient(empty, time.Second); err == nil {
		t.Fatal("garbage CA file accepted")
	}
}

func TestFetchAssertionKeys(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"keys":[]}`))
	}))
	defer srv.Close()
	// plain http to loopback: allowed (local development cloud)
	c := NewCloudClientWithBaseURL(srv.URL+"/api/v2/cloud", nil)
	body, err := c.FetchAssertionKeys(context.Background())
	if err != nil || string(body) != `{"keys":[]}` || gotPath != "/api/v2/cloud/assertion-keys" {
		t.Fatalf("body=%q path=%q err=%v", body, gotPath, err)
	}
	// plain http to a non-loopback host: refused before any request is made
	c = NewCloudClientWithBaseURL("http://cloud.example/api/v2/cloud", nil)
	if _, err := c.FetchAssertionKeys(context.Background()); err == nil || !strings.Contains(err.Error(), "https required") {
		t.Fatalf("got %v, want https required", err)
	}
}

func TestFetchAssertionKeysStatusAndSize(t *testing.T) {
	var h http.HandlerFunc
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h(w, r) }))
	defer srv.Close()
	c := NewCloudClientWithBaseURL(srv.URL, nil)

	h = func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusInternalServerError) }
	if _, err := c.FetchAssertionKeys(context.Background()); err == nil {
		t.Fatal("500 accepted")
	}
	h = func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", maxKeySetBytes+10)))
	}
	if _, err := c.FetchAssertionKeys(context.Background()); err == nil {
		t.Fatal("oversized body accepted")
	}
}

// The key set is fetched from AssertionKeysBaseURL (the cloud's pinned
// listener) when configured, over the pinned channel, and falls back to
// CloudAPIBaseURL otherwise.
func TestNewPinnedCloudClientUsesAssertionKeysBaseURL(t *testing.T) {
	ca := newCA(t, "pinned")
	leaf := issue(t, &x509.Certificate{
		Subject: pkix.Name{CommonName: "cloud"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, &ca)
	var path string
	pinned := serve(t, leaf, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"keys":[]}`))
	}))
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ca_chain.pem"), ca.pem(), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := utils.Config{CertDir: dir, CloudAPIBaseURL: "https://127.0.0.1:1/unused", AssertionKeysBaseURL: pinned.URL + "/api/v2/cloud"}
	cc, err := NewPinnedCloudClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cc.FetchAssertionKeys(context.Background()); err != nil || path != "/api/v2/cloud/assertion-keys" {
		t.Fatalf("path=%q err=%v", path, err)
	}
	// Unset: falls back to the cloud API base URL.
	cfg.AssertionKeysBaseURL = ""
	cfg.CloudAPIBaseURL = pinned.URL + "/other"
	cc, _ = NewPinnedCloudClient(cfg)
	if _, err := cc.FetchAssertionKeys(context.Background()); err != nil || path != "/other/assertion-keys" {
		t.Fatalf("fallback path=%q err=%v", path, err)
	}
	// A server certificate from another CA on the pinned URL is refused.
	other := newCA(t, "pinned")
	imp := issue(t, &x509.Certificate{
		Subject: pkix.Name{CommonName: "cloud"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, &other)
	cfg.AssertionKeysBaseURL = serve(t, imp, ok).URL + "/api/v2/cloud"
	cc, _ = NewPinnedCloudClient(cfg)
	if _, err := cc.FetchAssertionKeys(context.Background()); err == nil {
		t.Fatal("key set fetched over a server certificate from a different CA")
	}
}
