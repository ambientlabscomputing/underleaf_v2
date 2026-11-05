package clients

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

// PinnedTLSConfig returns a TLS client config that trusts only the given CA
// pool (never the system roots), for connections to the cloud that must be
// authenticated against the CA chain pinned at registration (ca_chain.pem).
//
// Verification is done in VerifyConnection so one rule covers two server
// certificates:
//
//   - A leaf that carries subject alternative names (a proper server leaf
//     issued by the CA) is verified for the dialed host name too.
//   - A certificate with NO names is the cloud's current setup: nginx presents
//     the root CA certificate itself, which has no SAN (RFD 5, Open questions,
//     "Channel for fetching the assertion key set"). Standard host name
//     verification can never pass for it, so only the chain is verified: the
//     peer must be, or be signed by, the pinned CA. This is weaker (no binding
//     to the host name) and is the documented interim until the cloud presents
//     a CA-issued server leaf; it upgrades itself the moment one is served.
//
// Certificates are also required to be usable for server authentication, so a
// client certificate issued by the same CA (EKU clientAuth) cannot pose as the
// server.
func PinnedTLSConfig(pool *x509.CertPool) *tls.Config {
	return pinnedTLSConfig(pool, time.Now, "")
}

// pinnedTLSConfig builds the config. host, if set, is the name to verify; it
// exists because crypto/tls reports an empty ConnectionState.ServerName when
// the dialed host is an IP address (no SNI is sent for IPs).
func pinnedTLSConfig(pool *x509.CertPool, now func() time.Time, host string) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		// The built-in verification is replaced, not skipped: VerifyConnection
		// below runs on every handshake and fails it on any error.
		InsecureSkipVerify: true, //nolint:gosec // verified in VerifyConnection
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("pinned TLS: server presented no certificate")
			}
			leaf := cs.PeerCertificates[0]
			inter := x509.NewCertPool()
			for _, c := range cs.PeerCertificates[1:] {
				inter.AddCert(c)
			}
			opts := x509.VerifyOptions{
				Roots:         pool,
				Intermediates: inter,
				CurrentTime:   now(),
				KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			}
			if hasNames(leaf) {
				// An empty ServerName would silently skip the host check in
				// x509, so refuse it: callers must set ServerName (the HTTP
				// client from NewPinnedHTTPClient always does, IPs included).
				name := host
				if name == "" {
					name = cs.ServerName
				}
				if name == "" {
					return errors.New("pinned TLS: no server name to verify against the certificate")
				}
				opts.DNSName = name
			}
			if _, err := leaf.Verify(opts); err != nil {
				return fmt.Errorf("pinned TLS: %w", err)
			}
			return nil
		},
	}
}

func hasNames(c *x509.Certificate) bool {
	return len(c.DNSNames)+len(c.IPAddresses)+len(c.EmailAddresses)+len(c.URIs) > 0
}

// NewPinnedHTTPClient builds an HTTP client that trusts only the PEM CA
// certificates in caChainPath.
func NewPinnedHTTPClient(caChainPath string, timeout time.Duration) (*http.Client, error) {
	pemBytes, err := os.ReadFile(caChainPath)
	if err != nil {
		return nil, fmt.Errorf("read pinned CA chain: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, errors.New("pinned CA chain contains no certificate")
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			// net/http leaves the TLS ServerName empty when the URL host is an
			// IP address, so dial ourselves and hand the verifier the host.
			DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				cfg := pinnedTLSConfig(pool, time.Now, host)
				cfg.ServerName = host
				return (&tls.Dialer{Config: cfg}).DialContext(ctx, network, addr)
			},
		},
	}, nil
}
