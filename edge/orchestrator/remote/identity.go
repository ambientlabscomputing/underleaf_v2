package remote

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// ClusterIDFromCertDir returns this cluster's id: the subject common name of
// the cloud-issued cluster certificate in certDir (cloud_api signs it with
// CN = the cluster id, and the cloud uses that same id as the assertion
// audience). An unregistered cluster has no such file and so no identity; the
// Verifier then rejects everything.
func ClusterIDFromCertDir(certDir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(certDir, string(types.CertFileNameClusterCert)))
	if err != nil {
		return "", fmt.Errorf("read cluster certificate: %w", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return "", errors.New("cluster certificate is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse cluster certificate: %w", err)
	}
	if cert.Subject.CommonName == "" {
		return "", errors.New("cluster certificate has no common name")
	}
	return cert.Subject.CommonName, nil
}

// ClusterIDProvider returns a function suitable for VerifierConfig.Audience
// that reads the cluster id from certDir and caches a successful answer for ttl
// (an id that cannot be read is never cached, so a cluster that registers while
// running starts verifying within one call).
func ClusterIDProvider(certDir string, ttl time.Duration, now func() time.Time) func() (string, error) {
	if now == nil {
		now = time.Now
	}
	var (
		mu     sync.Mutex
		id     string
		expiry time.Time
	)
	return func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if id != "" && now().Before(expiry) {
			return id, nil
		}
		got, err := ClusterIDFromCertDir(certDir)
		if err != nil {
			id = ""
			return "", err
		}
		id, expiry = got, now().Add(ttl)
		return id, nil
	}
}
