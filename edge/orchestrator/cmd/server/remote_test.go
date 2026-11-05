package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/rest"
	"github.com/ambientlabscomputing/underleaf_v2/shared/assertion"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func writePEMCert(t *testing.T, path, cn string) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Without UNDERLEAF_REMOTE_ADDR nothing happens: no listener, no verifier, no fetch.
func TestRemoteAccessIsOffByDefault(t *testing.T) {
	t.Setenv(rest.EnvRemoteAddr, "")
	var fetched bool
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fetched = true }))
	defer cloud.Close()
	srv := &rest.OrchestratorRESTServer{}
	startRemoteAccess(context.Background(), srv, utils.Config{CertDir: t.TempDir(), CloudAPIBaseURL: cloud.URL})
	if srv.Remote != nil {
		t.Fatal("a verifier was configured with remote access off")
	}
	time.Sleep(100 * time.Millisecond)
	if fetched {
		t.Fatal("the cloud was contacted with remote access off")
	}
}

// The real wiring: pinned cloud client (loopback http allowed), key cache with
// persistence, cluster id from the cluster cert, the actual remote listener.
func TestRemoteAccessWiring(t *testing.T) {
	certDir := t.TempDir()
	writePEMCert(t, filepath.Join(certDir, "cluster_cert.pem"), "cluster_wired")
	writePEMCert(t, filepath.Join(certDir, "ca_chain.pem"), "Test CA")

	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	kid := assertion.Thumbprint(pub)
	signer, _ := assertion.NewSigner(kid, priv)
	jwks, _ := json.Marshal(assertion.KeySet{Keys: []assertion.JWK{assertion.NewJWK(kid, pub, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))}})
	var path string
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write(jwks)
	}))
	defer cloud.Close()

	addr := "127.0.0.1:" + strconv.Itoa(freePort(t))
	t.Setenv(rest.EnvRemoteAddr, addr)
	srv := &rest.OrchestratorRESTServer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startRemoteAccess(ctx, srv, utils.Config{CertDir: certDir, CloudAPIBaseURL: cloud.URL + "/api/v2/cloud"})

	mint := func(aud, jti string) string {
		now := time.Now()
		tok, _ := signer.Sign(assertion.Claims{Issuer: assertion.DefaultIssuer, Subject: "acct", Audience: aud,
			Scopes: []string{"read"}, IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix(), JTI: jti})
		return tok
	}
	call := func(tok string) int {
		req, _ := http.NewRequest("GET", "http://"+addr+"/api/v1/health", nil)
		if tok != "" {
			req.Header.Set(assertion.HeaderName, tok)
		}
		var resp *http.Response
		var err error
		for i := 0; i < 100; i++ { // the listener and the first key fetch start asynchronously
			if resp, err = http.DefaultClient.Do(req); err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode != http.StatusUnauthorized || tok == "" {
					return resp.StatusCode
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode
	}

	if code := call(mint("cluster_wired", "w1")); code != 200 {
		t.Fatalf("valid assertion: %d", code)
	}
	if path != "/api/v2/cloud/assertion-keys" {
		t.Fatalf("keys fetched from %q", path)
	}
	if code := call(""); code != 401 {
		t.Fatalf("no assertion: %d", code)
	}
	if code := call(mint("cluster_other", "w2")); code != 401 {
		t.Fatalf("wrong audience: %d", code)
	}
	if _, err := os.Stat(filepath.Join(certDir, assertionKeysFile)); err != nil {
		t.Fatalf("key set not persisted: %v", err)
	}
}
