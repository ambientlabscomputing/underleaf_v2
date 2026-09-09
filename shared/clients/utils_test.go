package clients

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// generateSelfSignedPEM returns a self-signed cert PEM and its key PEM.
// GetTLSConfig only parses these individually (tls.LoadX509KeyPair, then a
// separate AppendCertsFromPEM for the "CA" file) — it never validates that
// the client cert chains to the CA — so a self-signed pair is sufficient to
// exercise both the happy path and the pairing between cert and key.
func generateSelfSignedPEM(t *testing.T, commonName string) (certPEM, keyPEM []byte) {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &privKey.PublicKey, privKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privKey)})
	return certPEM, keyPEM
}

func TestGetTLSConfig_MissingFiles(t *testing.T) {
	cfg := utils.Config{ConfigType: utils.OrchestratorConfig, CertDir: t.TempDir()}

	tlsConfig, err := GetTLSConfig(cfg)
	if err == nil {
		t.Fatal("expected an error for missing cert files, got nil (should not panic either)")
	}
	if tlsConfig != nil {
		t.Errorf("expected nil *tls.Config on error, got %+v", tlsConfig)
	}
}

func TestGetTLSConfig_MalformedCA(t *testing.T) {
	dir := t.TempDir()
	certPEM, keyPEM := generateSelfSignedPEM(t, "cluster_test")

	writeCertFile(t, dir, types.CertFileNameClusterCert, certPEM)
	writeCertFile(t, dir, types.CertFileNameClusterKey, keyPEM)
	writeCertFile(t, dir, types.CertFileNameCAChain, []byte("not a valid pem"))

	cfg := utils.Config{ConfigType: utils.OrchestratorConfig, CertDir: dir}
	if _, err := GetTLSConfig(cfg); err == nil {
		t.Fatal("expected an error for a malformed CA file, got nil")
	}
}

func TestGetTLSConfig_HappyPath(t *testing.T) {
	dir := t.TempDir()
	certPEM, keyPEM := generateSelfSignedPEM(t, "cluster_test")
	caPEM, _ := generateSelfSignedPEM(t, "test-ca")

	writeCertFile(t, dir, types.CertFileNameClusterCert, certPEM)
	writeCertFile(t, dir, types.CertFileNameClusterKey, keyPEM)
	writeCertFile(t, dir, types.CertFileNameCAChain, caPEM)

	cfg := utils.Config{ConfigType: utils.OrchestratorConfig, CertDir: dir}

	tlsConfig, err := GetTLSConfig(cfg)
	if err != nil {
		t.Fatalf("GetTLSConfig returned error: %v", err)
	}
	if len(tlsConfig.Certificates) != 1 {
		t.Fatalf("expected 1 client certificate, got %d", len(tlsConfig.Certificates))
	}
	if tlsConfig.RootCAs == nil {
		t.Fatal("expected a non-nil RootCAs pool")
	}
}

func writeCertFile(t *testing.T, dir string, name types.CertFileName, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, string(name)), data, 0600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
