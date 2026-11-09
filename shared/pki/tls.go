package pki

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

type tlsKeyPair struct {
	der   []byte
	caDER []byte
	key   *ecdsa.PrivateKey
	leaf  *x509.Certificate
	cert  *tls.Certificate
}

func (k *tlsKeyPair) certificate() *tls.Certificate {
	if k.cert != nil {
		return k.cert
	}
	k.cert = &tls.Certificate{
		// leaf followed by the CA, so a client pinning the CA's fingerprint
		// sees it in the presented chain.
		Certificate: [][]byte{k.der, k.caDER},
		PrivateKey:  k.key,
		Leaf:        k.leaf,
	}
	return k.cert
}

func addSANs(tmpl *x509.Certificate, hosts []string) {
	seen := map[string]bool{}
	for _, h := range hosts {
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
}

// ServerCertSource hands out the manager's listener certificate, re-issuing it
// shortly before it expires so a long-running manager never serves a lapsed
// certificate. Nodes verify it against the cluster CA and the fixed ServerName,
// so re-issuing needs no coordination with them.
type ServerCertSource struct {
	ca    *CA
	hosts []string
	now   func() time.Time

	mu  sync.Mutex
	cur *tlsKeyPair
}

// NewServerCertSource creates a source whose certificate carries ServerName
// plus hosts (the advertised address) as SANs.
func NewServerCertSource(ca *CA, hosts []string, now func() time.Time) *ServerCertSource {
	if now == nil {
		now = time.Now
	}
	return &ServerCertSource{ca: ca, hosts: hosts, now: now}
}

// GetCertificate implements tls.Config.GetCertificate.
func (s *ServerCertSource) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.cur == nil || s.cur.leaf.NotAfter.Sub(now) < ExpiryWarning {
		kp, err := s.ca.issueServer(s.hosts, now)
		if err != nil {
			return nil, err
		}
		s.cur = kp
	}
	return s.cur.certificate(), nil
}

// CertPool builds a pool from a PEM bundle of CA certificates.
func CertPool(bundlePEM []byte) (*x509.CertPool, error) {
	certs, err := ParseCertificates(bundlePEM)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	for _, c := range certs {
		pool.AddCert(c)
	}
	return pool, nil
}

// RevokedFunc reports whether a certificate serial (lowercase hex) is on the
// deny list.
type RevokedFunc func(serial string) bool

// ClusterServerTLS is the TLS configuration for the cluster listener's mTLS
// side: a client certificate chaining to the cluster trust bundle is required
// at the handshake, and a certificate on the deny list is refused even though
// it still verifies.
func ClusterServerTLS(src *ServerCertSource, trustBundlePEM []byte, revoked RevokedFunc) (*tls.Config, error) {
	pool, err := CertPool(trustBundlePEM)
	if err != nil {
		return nil, fmt.Errorf("pki: trust bundle: %w", err)
	}
	return &tls.Config{
		MinVersion:     tls.VersionTLS13,
		GetCertificate: src.GetCertificate,
		ClientAuth:     tls.RequireAndVerifyClientCert,
		ClientCAs:      pool,
		NextProtos:     []string{"h2"},
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("pki: no client certificate")
			}
			if revoked != nil && revoked(SerialString(cs.PeerCertificates[0])) {
				return errors.New("pki: client certificate has been revoked")
			}
			return nil
		},
	}, nil
}

// JoinServerTLS is the TLS configuration for the join side of the cluster
// listener. It authenticates the *server* only, because a node that is joining
// has no certificate yet. It never negotiates h2, so a client without a
// certificate cannot reach any gRPC service; see utils.ListenCluster for how
// connections are routed.
func JoinServerTLS(src *ServerCertSource) *tls.Config {
	return &tls.Config{
		MinVersion:     tls.VersionTLS13,
		GetCertificate: src.GetCertificate,
		ClientAuth:     tls.NoClientCert,
		NextProtos:     []string{"http/1.1"},
	}
}

// PinnedClientTLS is the TLS configuration a joining node uses *before* it has
// any trust anchor: it accepts the server only if the presented chain contains
// a CA certificate whose fingerprint equals the one in the join string and the
// server certificate is signed by that CA. The check runs during the handshake,
// so no application byte (in particular, the join secret) is sent to a server
// that fails it.
//
// Hostnames are deliberately not checked: the pinned CA, not the DNS name, is
// the root of trust (as with kubeadm's discovery hash), and the manager's
// address may be a bare IP or change.
func PinnedClientTLS(fp [32]byte) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		// InsecureSkipVerify turns off the default verification only so that
		// VerifyPeerCertificate below, which is strictly stronger for this
		// purpose (pinned CA), is the sole judge.
		InsecureSkipVerify: true, //nolint:gosec // replaced by VerifyPeerCertificate
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			return verifyPinned(raw, fp, time.Now())
		},
		NextProtos: []string{"http/1.1"},
	}
}

func verifyPinned(raw [][]byte, fp [32]byte, now time.Time) error {
	if len(raw) == 0 {
		return errors.New("pki: server presented no certificate")
	}
	certs := make([]*x509.Certificate, 0, len(raw))
	for _, r := range raw {
		c, err := x509.ParseCertificate(r)
		if err != nil {
			return fmt.Errorf("pki: parse server certificate: %w", err)
		}
		certs = append(certs, c)
	}
	var ca *x509.Certificate
	for _, c := range certs[1:] {
		if Fingerprint(c) == fp {
			ca = c
			break
		}
	}
	if ca == nil && Fingerprint(certs[0]) == fp {
		ca = certs[0]
	}
	if ca == nil {
		return errors.New("pki: server certificate chain does not contain the pinned CA (wrong cluster, or the connection is being intercepted)")
	}
	if !ca.IsCA || !ca.BasicConstraintsValid {
		return errors.New("pki: pinned certificate is not a CA")
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	_, err := certs[0].Verify(x509.VerifyOptions{
		Roots:       pool,
		CurrentTime: now,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	if err != nil {
		return fmt.Errorf("pki: server certificate is not signed by the pinned CA: %w", err)
	}
	return nil
}

// NodeClientTLS is the TLS configuration a member node uses toward the cluster
// listener: it verifies the manager against the cluster trust bundle (and the
// fixed ServerName) and presents the node certificate returned by getCert.
// getCert is a callback so a renewed certificate is picked up by new
// connections without a restart.
func NodeClientTLS(trustBundlePEM []byte, getCert func(*tls.CertificateRequestInfo) (*tls.Certificate, error)) (*tls.Config, error) {
	pool, err := CertPool(trustBundlePEM)
	if err != nil {
		return nil, fmt.Errorf("pki: trust bundle: %w", err)
	}
	return &tls.Config{
		MinVersion:           tls.VersionTLS13,
		RootCAs:              pool,
		ServerName:           ServerName,
		GetClientCertificate: getCert,
		NextProtos:           []string{"h2"},
	}, nil
}

func verifyClientOpts(roots *x509.CertPool) x509.VerifyOptions {
	return x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
}
