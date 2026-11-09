// Package pki implements the cluster-local PKI described in RFD 5: a
// self-contained CA that lives on the manager, per-node certificates signed by
// it, the join string a new node is enrolled with, and the TLS configurations
// both ends use.
//
// It is deliberately unrelated to the cloud CA (RFD 5, "Cluster CA"): local
// trust must not depend on the cloud, so nothing here reads cloud-issued
// certificates and the cloud never sees this CA's key.
package pki

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const (
	// CALifetime is how long the cluster CA is valid. Rotating a CA is the
	// dangerous operation, so it is pushed beyond the product's horizon.
	CALifetime = 10 * 365 * 24 * time.Hour
	// NodeCertLifetime matches the cloud-issued cluster certs (730 days) so
	// there is one lifetime to reason about.
	NodeCertLifetime = 730 * 24 * time.Hour
	// RenewalWindow is how long before expiry a node starts renewing. It is
	// deliberately wider than the cloud flow's 30 days: a node gets ~90 daily
	// attempts before a renewal can fail.
	RenewalWindow = 90 * 24 * time.Hour
	// ExpiryWarning is when `orcli nodes ls` starts warning.
	ExpiryWarning = 30 * 24 * time.Hour
	// ClockSkewBackdate is how far NotBefore is moved into the past.
	ClockSkewBackdate = 5 * time.Minute
	// ServerCertLifetime is the lifetime of the manager's listener certificate.
	// It is re-issued automatically (see ServerCertSource) so it never lapses.
	ServerCertLifetime = 365 * 24 * time.Hour

	// ServerName is the fixed name the cluster listener's certificate always
	// carries and that nodes verify it against after joining. Using a fixed
	// name keeps nodes working when the manager's address changes; the cluster
	// CA only ever signs this certificate for the manager, and node
	// certificates carry the client-auth usage only, so a node cannot
	// impersonate the manager.
	ServerName = "orchestrator.cluster.underleaf"

	caKeyFile       = "ca_key.pem"
	caCertFile      = "ca_cert.pem"
	trustBundleFile = "trust_bundle.pem"
)

var nodeIDPattern = regexp.MustCompile(`^node_[0-9a-z]{20}$`)

// ValidNodeID reports whether id looks like an id produced by
// types.GenerateID(types.NodeIDPrefix). The certificate's CN is the node id,
// so the manager refuses to sign anything else.
func ValidNodeID(id string) bool { return nodeIDPattern.MatchString(id) }

// Fingerprint returns the SHA-256 of the certificate's DER encoding.
func Fingerprint(cert *x509.Certificate) [32]byte { return sha256.Sum256(cert.Raw) }

// FormatFingerprint renders a fingerprint as "sha256:" + colon-separated hex.
func FormatFingerprint(fp [32]byte) string {
	h := hex.EncodeToString(fp[:])
	out := make([]byte, 0, len(h)+len(h)/2+8)
	out = append(out, "sha256:"...)
	for i := 0; i < len(h); i += 2 {
		if i > 0 {
			out = append(out, ':')
		}
		out = append(out, h[i:i+2]...)
	}
	return string(out)
}

// CA is the cluster-local certificate authority. The private key never leaves
// the manager: it is only ever written to a 0600 file in a 0700 directory and
// never serialised into any response.
type CA struct {
	Cert    *x509.Certificate
	CertPEM []byte
	key     crypto.Signer
}

// NewCA generates a fresh CA valid for CALifetime from now.
func NewCA(commonName string, now time.Time) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("pki: generate CA key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName, Organization: []string{"Underleaf cluster"}},
		NotBefore:             now.Add(-ClockSkewBackdate),
		NotAfter:              now.Add(CALifetime),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("pki: create CA certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &CA{
		Cert:    cert,
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		key:     key,
	}, nil
}

// Fingerprint is the CA certificate's SHA-256 fingerprint, the value pinned in
// the join string.
func (ca *CA) Fingerprint() [32]byte { return Fingerprint(ca.Cert) }

// LoadOrCreateCA loads the CA stored in dir, generating and persisting one on
// first use. The directory is created 0700 and the key written 0600. A
// partially present CA (only one of key/cert) is an error rather than
// something to silently regenerate over: replacing a CA would orphan every
// enrolled node.
func LoadOrCreateCA(dir string, now time.Time) (*CA, bool, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, false, fmt.Errorf("pki: create CA dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, false, fmt.Errorf("pki: secure CA dir: %w", err)
	}
	certPath := filepath.Join(dir, caCertFile)
	keyPath := filepath.Join(dir, caKeyFile)
	_, certErr := os.Stat(certPath)
	_, keyErr := os.Stat(keyPath)
	switch {
	case certErr == nil && keyErr == nil:
		ca, err := loadCA(certPath, keyPath)
		return ca, false, err
	case certErr == nil || keyErr == nil:
		return nil, false, fmt.Errorf("pki: CA in %s is incomplete (need both %s and %s); refusing to regenerate", dir, caCertFile, caKeyFile)
	}

	ca, err := NewCA("Underleaf cluster CA", now)
	if err != nil {
		return nil, false, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(ca.key)
	if err != nil {
		return nil, false, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := writeFileAtomic(keyPath, keyPEM, 0o600); err != nil {
		return nil, false, err
	}
	if err := writeFileAtomic(certPath, ca.CertPEM, 0o644); err != nil {
		return nil, false, err
	}
	if err := writeFileAtomic(filepath.Join(dir, trustBundleFile), ca.CertPEM, 0o644); err != nil {
		return nil, false, err
	}
	return ca, true, nil
}

func loadCA(certPath, keyPath string) (*CA, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("pki: %s: no certificate PEM block", certPath)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("pki: parse CA certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	kb, _ := pem.Decode(keyPEM)
	if kb == nil {
		return nil, fmt.Errorf("pki: %s: no key PEM block", keyPath)
	}
	k, err := x509.ParsePKCS8PrivateKey(kb.Bytes)
	if err != nil {
		return nil, fmt.Errorf("pki: parse CA key: %w", err)
	}
	signer, ok := k.(crypto.Signer)
	if !ok {
		return nil, errors.New("pki: CA key is not a signing key")
	}
	pub, ok := signer.Public().(*ecdsa.PublicKey)
	cpub, cok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || !cok || !pub.Equal(cpub) {
		return nil, errors.New("pki: CA key does not match CA certificate")
	}
	return &CA{Cert: cert, CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), key: signer}, nil
}

// TrustBundle returns the PEM bundle of every CA this cluster trusts. It always
// contains the active CA and can hold more (a rotation is then possible
// later without a flag day: add the new CA, re-issue, drop the old one).
func TrustBundle(dir string, active *CA) ([]byte, error) {
	var out []byte
	seen := map[[32]byte]bool{}
	add := func(c *x509.Certificate) {
		fp := Fingerprint(c)
		if seen[fp] {
			return
		}
		seen[fp] = true
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})...)
	}
	add(active.Cert)
	data, err := os.ReadFile(filepath.Join(dir, trustBundleFile))
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	certs, err := ParseCertificates(data)
	if err != nil {
		return nil, fmt.Errorf("pki: trust bundle: %w", err)
	}
	for _, c := range certs {
		add(c)
	}
	return out, nil
}

// ParseCertificates parses every CERTIFICATE block in data.
func ParseCertificates(data []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certs = append(certs, c)
	}
	if len(certs) == 0 {
		return nil, errors.New("no certificates found")
	}
	return certs, nil
}

// NodeCert is a freshly issued node certificate.
type NodeCert struct {
	PEM       []byte
	Cert      *x509.Certificate
	Serial    string // lowercase hex, the key of the deny list
	NotAfter  time.Time
	Subject   string
	NotBefore time.Time
}

// SerialString renders a certificate serial the way the deny list keys it.
func SerialString(c *x509.Certificate) string { return c.SerialNumber.Text(16) }

// ParseCSR decodes and verifies (signature included) a PEM certificate request
// and checks its CN is a well-formed node id. The CN is the *only* thing taken
// from the CSR besides the public key: names, SANs, usages and extensions the
// requester asks for are ignored.
func ParseCSR(csrPEM []byte) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode(csrPEM)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, errors.New("pki: no certificate request PEM block")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("pki: parse CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("pki: CSR signature: %w", err)
	}
	if !ValidNodeID(csr.Subject.CommonName) {
		return nil, fmt.Errorf("pki: CSR common name %q is not a valid node id", csr.Subject.CommonName)
	}
	switch pub := csr.PublicKey.(type) {
	case *ecdsa.PublicKey:
		if pub.Curve.Params().BitSize < 256 {
			return nil, errors.New("pki: CSR key too weak")
		}
	default:
		// Nodes generate ECDSA keys; do not accept anything else so the
		// parameters of every cluster credential are known.
		return nil, errors.New("pki: CSR must carry an ECDSA public key")
	}
	return csr, nil
}

// SignNode issues a client-auth-only certificate with CN = the CSR's node id,
// valid for NodeCertLifetime with NotBefore backdated for clock skew.
func (ca *CA) SignNode(csr *x509.CertificateRequest, now time.Time) (*NodeCert, error) {
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: csr.Subject.CommonName, Organization: []string{"Underleaf node"}},
		NotBefore:             now.Add(-ClockSkewBackdate),
		NotAfter:              now.Add(NodeCertLifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, csr.PublicKey, ca.key)
	if err != nil {
		return nil, fmt.Errorf("pki: sign node certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &NodeCert{
		PEM:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		Cert:      cert,
		Serial:    SerialString(cert),
		NotAfter:  cert.NotAfter,
		NotBefore: cert.NotBefore,
		Subject:   cert.Subject.CommonName,
	}, nil
}

// issueServer signs the manager's listener certificate for the given SANs.
func (ca *CA) issueServer(hosts []string, now time.Time) (*tlsKeyPair, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: ServerName},
		NotBefore:             now.Add(-ClockSkewBackdate),
		NotAfter:              now.Add(ServerCertLifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	addSANs(tmpl, append([]string{ServerName}, hosts...))
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.key)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	return &tlsKeyPair{der: der, key: key, leaf: leaf, caDER: ca.Cert.Raw}, nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 127)
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("pki: serial: %w", err)
	}
	return n.Add(n, big.NewInt(1)), nil // never zero
}

// GenerateNodeKey creates a node's ECDSA key and a CSR for it with CN = nodeID.
func GenerateNodeKey(nodeID string) (key *ecdsa.PrivateKey, csrPEM []byte, err error) {
	if !ValidNodeID(nodeID) {
		return nil, nil, fmt.Errorf("pki: %q is not a valid node id", nodeID)
	}
	key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: nodeID},
	}, key)
	if err != nil {
		return nil, nil, err
	}
	return key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), nil
}

// MarshalKeyPEM encodes an ECDSA private key as PKCS#8 PEM.
func MarshalKeyPEM(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// writeFileAtomic writes data to a temp file in the same directory with the
// final permissions, then renames it into place, so readers never see a
// truncated file and a key is never briefly world-readable.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op after a successful rename
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// WriteFileAtomic is the exported form for the packages that persist node state.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	return writeFileAtomic(path, data, perm)
}
