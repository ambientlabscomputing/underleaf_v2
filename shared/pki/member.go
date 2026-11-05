package pki

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	memberStateFile  = "cluster.json"
	memberCredsFile  = "node.pem"      // private key + certificate in one file, so a swap is atomic
	memberBundleFile = "ca_bundle.pem" // every CA this node trusts
)

// ErrNotMember means no cluster membership state exists in the directory.
var ErrNotMember = errors.New("pki: this node has not joined a cluster")

// MemberState is the non-secret description of a node's membership.
type MemberState struct {
	ManagerAddr   string    `json:"manager_addr"`
	NodeID        string    `json:"node_id"`
	CAFingerprint string    `json:"ca_fingerprint"`
	JoinedAt      time.Time `json:"joined_at"`
}

// Member is a node that has joined a cluster: its identity plus the
// credentials it presents. The certificate can be swapped at runtime
// (renewal) without disturbing readers.
type Member struct {
	Dir   string
	State MemberState

	mu     sync.RWMutex
	cert   tls.Certificate
	bundle []byte
}

// LoadMember reads membership state from dir. It returns ErrNotMember when the
// node has not joined (no cluster.json). The state file is written last by
// SaveMember, so its presence implies complete credentials.
func LoadMember(dir string) (*Member, error) {
	raw, err := os.ReadFile(filepath.Join(dir, memberStateFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotMember
		}
		return nil, err
	}
	var st MemberState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("pki: %s: %w", memberStateFile, err)
	}
	if !ValidNodeID(st.NodeID) || st.ManagerAddr == "" {
		return nil, fmt.Errorf("pki: %s is incomplete", filepath.Join(dir, memberStateFile))
	}
	m := &Member{Dir: dir, State: st}
	creds, err := os.ReadFile(filepath.Join(dir, memberCredsFile))
	if err != nil {
		return nil, fmt.Errorf("pki: node credentials: %w", err)
	}
	bundle, err := os.ReadFile(filepath.Join(dir, memberBundleFile))
	if err != nil {
		return nil, fmt.Errorf("pki: CA bundle: %w", err)
	}
	if err := m.set(creds, bundle); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Member) set(credsPEM, bundlePEM []byte) error {
	cert, err := tls.X509KeyPair(credsPEM, credsPEM)
	if err != nil {
		return fmt.Errorf("pki: node credentials: %w", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return err
	}
	cert.Leaf = leaf
	if _, err := ParseCertificates(bundlePEM); err != nil {
		return fmt.Errorf("pki: CA bundle: %w", err)
	}
	m.mu.Lock()
	m.cert = cert
	m.bundle = append([]byte(nil), bundlePEM...)
	m.mu.Unlock()
	return nil
}

// GetClientCertificate implements tls.Config.GetClientCertificate.
func (m *Member) GetClientCertificate(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c := m.cert
	return &c, nil
}

// NotAfter is when the current node certificate expires.
func (m *Member) NotAfter() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cert.Leaf.NotAfter
}

// Serial is the current certificate's serial (lowercase hex).
func (m *Member) Serial() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return SerialString(m.cert.Leaf)
}

// Bundle returns the PEM bundle of trusted CAs.
func (m *Member) Bundle() []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]byte(nil), m.bundle...)
}

// TLSConfig is the mTLS client configuration toward the cluster listener.
func (m *Member) TLSConfig() (*tls.Config, error) {
	return NodeClientTLS(m.Bundle(), m.GetClientCertificate)
}

// Install persists a renewed certificate (with its key and the CA bundle the
// manager returned) atomically and makes it current.
func (m *Member) Install(key *ecdsa.PrivateKey, certPEM, bundlePEM []byte) error {
	creds, err := credsPEM(key, certPEM)
	if err != nil {
		return err
	}
	// Validate before touching disk.
	if _, err := tls.X509KeyPair(creds, creds); err != nil {
		return fmt.Errorf("pki: renewed certificate does not match key: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(m.Dir, memberBundleFile), bundlePEM, 0o644); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(m.Dir, memberCredsFile), creds, 0o600); err != nil {
		return err
	}
	return m.set(creds, bundlePEM)
}

func credsPEM(key *ecdsa.PrivateKey, certPEM []byte) ([]byte, error) {
	keyPEM, err := MarshalKeyPEM(key)
	if err != nil {
		return nil, err
	}
	return append(keyPEM, certPEM...), nil
}

// SaveMember persists a freshly enrolled node. The state file goes last so a
// crash mid-way leaves "not a member" rather than a half-written identity.
func SaveMember(dir string, st MemberState, key *ecdsa.PrivateKey, certPEM, bundlePEM []byte) (*Member, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	creds, err := credsPEM(key, certPEM)
	if err != nil {
		return nil, err
	}
	if _, err := tls.X509KeyPair(creds, creds); err != nil {
		return nil, fmt.Errorf("pki: issued certificate does not match key: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(dir, memberBundleFile), bundlePEM, 0o644); err != nil {
		return nil, err
	}
	if err := writeFileAtomic(filepath.Join(dir, memberCredsFile), creds, 0o600); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(filepath.Join(dir, memberStateFile), raw, 0o600); err != nil {
		return nil, err
	}
	return LoadMember(dir)
}

// PeerNodeID extracts the authenticated node id (the certificate CN) from a
// completed mTLS handshake.
func PeerNodeID(cs tls.ConnectionState) (string, bool) {
	if len(cs.VerifiedChains) == 0 || len(cs.PeerCertificates) == 0 {
		return "", false
	}
	id := cs.PeerCertificates[0].Subject.CommonName
	return id, ValidNodeID(id)
}

// PEMBlockTypes is a small helper for tests and diagnostics.
func PEMBlockTypes(data []byte) []string {
	var out []string
	for {
		var b *pem.Block
		b, data = pem.Decode(data)
		if b == nil {
			return out
		}
		out = append(out, b.Type)
	}
}
