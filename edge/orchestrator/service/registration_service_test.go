package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

func TestRequestCertificate_PersistsAllFourFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req clients.IssueCertificateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.CSRPEM == "" {
			t.Error("expected a non-empty CSR PEM in the request")
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(clients.IssueCertificateResponse{
			CertificatePEM:     "cluster-cert-pem",
			NodeCertificatePEM: "node-cert-pem",
			CAChainPEM:         "ca-chain-pem",
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	s := &RegistrationService{cloudClient: clients.NewCloudClientWithBaseURL(srv.URL, nil)}

	certPEM, keyPEM, err := s.requestCertificate("one-time-token", "cluster_abc", dir, "node_123")
	if err != nil {
		t.Fatalf("requestCertificate returned error: %v", err)
	}
	if certPEM != "cluster-cert-pem" {
		t.Errorf("certPEM = %q, want %q", certPEM, "cluster-cert-pem")
	}
	if keyPEM == "" {
		t.Error("expected a non-empty generated key PEM")
	}

	wantFiles := map[types.CertFileName]string{
		types.CertFileNameClusterCert: "cluster-cert-pem",
		types.CertFileNameClusterKey:  keyPEM,
		types.CertFileNameCAChain:     "ca-chain-pem",
		types.CertFileNameNodeCert:    "node-cert-pem",
	}
	for name, want := range wantFiles {
		got, err := os.ReadFile(filepath.Join(dir, string(name)))
		if err != nil {
			t.Errorf("reading %s: %v", name, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s content = %q, want %q", name, got, want)
		}
	}
}

func TestRequestCertificate_ReturnsErrorWhenPersistFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(clients.IssueCertificateResponse{
			CertificatePEM:     "cluster-cert-pem",
			NodeCertificatePEM: "node-cert-pem",
			CAChainPEM:         "ca-chain-pem",
		})
	}))
	defer srv.Close()

	// A regular file in place of certDir makes os.MkdirAll fail, since it
	// can't create a directory where a file already exists.
	dir := t.TempDir()
	blockedPath := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blockedPath, []byte("x"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	s := &RegistrationService{cloudClient: clients.NewCloudClientWithBaseURL(srv.URL, nil)}

	if _, _, err := s.requestCertificate("one-time-token", "cluster_abc", blockedPath, "node_123"); err == nil {
		t.Fatal("expected an error when cert files can't be persisted, got nil")
	}
}

func TestRequestCertificate_PropagatesCloudClientError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	s := &RegistrationService{cloudClient: clients.NewCloudClientWithBaseURL(srv.URL, nil)}

	if _, _, err := s.requestCertificate("bad-token", "cluster_abc", t.TempDir(), "node_123"); err == nil {
		t.Fatal("expected an error when cloud-api rejects the request, got nil")
	}
}

// writeTestCert writes a self-signed cert (CN=commonName, expiring in
// validFor) as cluster_cert.pem in dir.
func writeTestCert(t *testing.T, dir, commonName string, validFor time.Duration) {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(validFor),
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &privKey.PublicKey, privKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	if err := os.WriteFile(filepath.Join(dir, string(types.CertFileNameClusterCert)), certPEM, 0600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
}

func TestCheckAndRenewCertificate_NoOpWhenNoCertExists(t *testing.T) {
	s := &RegistrationService{}
	if err := s.CheckAndRenewCertificate(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("expected no error when no cert has been issued yet, got: %v", err)
	}
}

func TestCheckAndRenewCertificate_NoOpWhenNotDue(t *testing.T) {
	dir := t.TempDir()
	writeTestCert(t, dir, "cluster_abc", 700*24*time.Hour) // well beyond the 30-day threshold

	// No cloudClient/agentClient configured -- if this reached the renewal
	// path it would nil-pointer panic, which fails the test regardless.
	s := &RegistrationService{}
	if err := s.CheckAndRenewCertificate(context.Background(), dir); err != nil {
		t.Fatalf("expected no error/no renewal attempt for a cert not yet due, got: %v", err)
	}
}

func TestCheckAndRenewCertificate_ErrorsOnMalformedCert(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, string(types.CertFileNameClusterCert)), []byte("not a cert"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	s := &RegistrationService{}
	if err := s.CheckAndRenewCertificate(context.Background(), dir); err == nil {
		t.Fatal("expected an error for a malformed cluster cert, got nil")
	}
}
