package clients

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
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
		panic(fmt.Sprintf("failed to load client certificate: %v", err))
	}

	caCert, err := os.ReadFile(caPath)
	if err != nil {
		panic(fmt.Sprintf("failed to read CA certificate: %v", err))
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		panic("failed to append CA certificate to pool")
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
