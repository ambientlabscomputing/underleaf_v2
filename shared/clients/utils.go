package clients

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

func GetTLSConfig(cfg utils.Config) (*tls.Config, error) {
	crtPath := ""
	if cfg.ConfigType == utils.OrchestratorConfig {
		crtPath = cfg.CertDir + "/" + string(types.CertFileNameClusterCert)
	} else {
		crtPath = cfg.CertDir + "/" + string(types.CertFileNameNodeCert)
	}
	keyPath := cfg.CertDir + "/" + string(types.CertFileNameClusterKey)
	caPath := cfg.CertDir + "/" + string(types.CertFileNameCAChain)

	clientCert, err := tls.LoadX509KeyPair(crtPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	caCert, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to append CA certificate to pool")
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caCertPool,
	}
	return tlsConfig, nil
}

func HttpClientWithCert(cfg utils.Config) (*http.Client, error) {
	tlsConfig, err := GetTLSConfig(cfg)
	if err != nil {
		return nil, err
	}

	hc := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
	return hc, nil
}

// ClusterIDFromCertDir returns this cluster's id: the subject common name of
// the cloud-issued cluster certificate in certDir (cloud_api signs it with
// CN = the cluster id, and the cloud uses that same id as the assertion
// audience and as the id of the cluster's rows). An unregistered cluster has no
// such file and so no identity.
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
