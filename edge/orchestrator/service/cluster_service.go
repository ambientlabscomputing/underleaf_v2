package service

import (
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/pki"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// Errors returned to the cluster listener's handlers. They are deliberately
// coarse: a join that fails because the secret was wrong, reused or expired
// answers the same way in every case, so the endpoint is not an oracle.
var (
	ErrJoinDenied    = errors.New("join denied")
	ErrBadJoin       = errors.New("invalid join request")
	ErrNodeForbidden = errors.New("node not permitted")
)

// ClusterService owns cluster membership (RFD 5, "Cluster membership"): the
// cluster-local CA, join tokens, node certificates, renewal, revocation, and
// the lifecycle of the cluster listener.
type ClusterService struct {
	repo  *repository.Repository
	caDir string

	// Now is injectable so tests can exercise expiry without sleeping.
	Now func() time.Time

	mu     sync.Mutex
	ca     *pki.CA
	source *pki.ServerCertSource

	denyMu sync.RWMutex
	deny   map[string]struct{}

	// StartListener, set by the process wiring, brings the cluster listener
	// up on first use. It must be idempotent.
	StartListener func(advertise string) error
	// OnNodeRemoved is called after `nodes rm` so live connections for the
	// node can be closed.
	OnNodeRemoved func(nodeID string)
	// OnCertsRevoked is called when a node's certificates are revoked
	// (`nodes rm`, or re-enrollment superseding them), so a link that was
	// authenticated with a now-dead certificate does not outlive it.
	OnCertsRevoked func(nodeID string)
	// Linked reports whether a node currently has an agent link (wired up by
	// the link hub; nil means "unknown", shown as not linked).
	Linked func(nodeID string) bool
	// PrimaryIP discovers the default advertised host; replaceable in tests.
	PrimaryIP func() (string, error)
	// ClusterPort is the port the cluster listener binds and, by default,
	// advertises.
	ClusterPort string
}

// NewClusterService creates the service. It does not touch disk until Start.
func NewClusterService(repo *repository.Repository, caDir, clusterPort string) *ClusterService {
	return &ClusterService{
		repo:        repo,
		caDir:       caDir,
		Now:         time.Now,
		deny:        map[string]struct{}{},
		PrimaryIP:   primaryIP,
		ClusterPort: clusterPort,
	}
}

// Start generates the cluster CA on first start (loading it afterwards) and
// restores the deny list. It does not open any listener.
func (s *ClusterService) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ca != nil {
		return nil
	}
	ca, created, err := pki.LoadOrCreateCA(s.caDir, s.Now())
	if err != nil {
		return fmt.Errorf("cluster: CA: %w", err)
	}
	s.ca = ca
	if created {
		fmt.Printf("[cluster] generated cluster CA %s (key stays on this machine in %s)\n", pki.FormatFingerprint(ca.Fingerprint()), s.caDir)
	}
	return s.refreshDenyLocked()
}

// ListenerEnabled reports whether `orcli nodes add` has ever enabled the
// cluster listener on this manager.
func (s *ClusterService) ListenerEnabled() bool {
	v, _ := s.repo.Cluster.GetSetting(repository.SettingListenerEnabled)
	return v == "1"
}

// Advertised is the address nodes are told to dial ("" if never set).
func (s *ClusterService) Advertised() string {
	v, _ := s.repo.Cluster.GetSetting(repository.SettingAdvertiseAddr)
	return v
}

// Fingerprint is the CA fingerprint pinned in join strings.
func (s *ClusterService) Fingerprint() ([32]byte, error) {
	if err := s.Start(); err != nil {
		return [32]byte{}, err
	}
	return s.ca.Fingerprint(), nil
}

// AddNodeResult is what `orcli nodes add` prints.
type AddNodeResult struct {
	JoinString  string
	Advertise   string
	Fingerprint [32]byte
	ExpiresAt   time.Time
}

// AddNode mints a single-use join string and (on first use) enables the
// cluster listener. advertise is host:port; empty uses the stored value, then
// this machine's primary address with the cluster port.
func (s *ClusterService) AddNode(advertise string) (*AddNodeResult, error) {
	if err := s.Start(); err != nil {
		return nil, err
	}
	adv, err := s.resolveAdvertise(advertise)
	if err != nil {
		return nil, err
	}
	if s.StartListener != nil {
		if err := s.StartListener(adv); err != nil {
			return nil, fmt.Errorf("cluster: could not start the cluster listener: %w", err)
		}
	}
	if err := s.repo.Cluster.SetSetting(repository.SettingAdvertiseAddr, adv); err != nil {
		return nil, err
	}
	if err := s.repo.Cluster.SetSetting(repository.SettingListenerEnabled, "1"); err != nil {
		return nil, err
	}

	secret, err := pki.NewJoinSecret()
	if err != nil {
		return nil, err
	}
	now := s.Now()
	expires := now.Add(pki.JoinTTL)
	// Only the hash is stored; the secret exists in the join string alone.
	if err := s.repo.Cluster.CreateJoinToken(pki.HashSecret(secret), now, expires); err != nil {
		return nil, err
	}
	_ = s.repo.Cluster.PruneJoinTokens(now.Add(-24 * time.Hour))

	fp := s.ca.Fingerprint()
	js, err := pki.JoinInfo{Addr: adv, Fingerprint: fp, Secret: secret, ExpiresAt: expires}.Encode()
	if err != nil {
		return nil, err
	}
	return &AddNodeResult{JoinString: js, Advertise: adv, Fingerprint: fp, ExpiresAt: expires}, nil
}

func (s *ClusterService) resolveAdvertise(advertise string) (string, error) {
	advertise = strings.TrimSpace(advertise)
	if advertise == "" {
		advertise = s.Advertised()
	}
	if advertise == "" {
		ip, err := s.PrimaryIP()
		if err != nil {
			return "", fmt.Errorf("cluster: could not determine this machine's address (%v): pass --advertise host[:port]", err)
		}
		advertise = ip
	}
	if _, _, err := net.SplitHostPort(advertise); err != nil {
		advertise = net.JoinHostPort(strings.Trim(advertise, "[]"), s.ClusterPort)
	}
	host, port, err := net.SplitHostPort(advertise)
	if err != nil || host == "" || port == "" {
		return "", fmt.Errorf("cluster: %q is not a valid host[:port]", advertise)
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsUnspecified() || ip.IsMulticast()) {
		return "", fmt.Errorf("cluster: %q is not an address nodes can dial", host)
	}
	return advertise, nil
}

// ServerTLS builds the cluster listener's two TLS configurations: mTLS for the
// control surface and server-auth-only for the join endpoint. The deny list is
// consulted on every handshake.
func (s *ClusterService) ServerTLS(advertise string) (mtls, join *tls.Config, err error) {
	if err := s.Start(); err != nil {
		return nil, nil, err
	}
	bundle, err := pki.TrustBundle(s.caDir, s.ca)
	if err != nil {
		return nil, nil, err
	}
	var hosts []string
	if h, _, err := net.SplitHostPort(advertise); err == nil {
		hosts = append(hosts, h)
	}
	src := pki.NewServerCertSource(s.ca, hosts, s.Now)
	mtls, err = pki.ClusterServerTLS(src, bundle, s.IsRevoked)
	if err != nil {
		return nil, nil, err
	}
	return mtls, pki.JoinServerTLS(src), nil
}

// Join handles an enrollment. See RFD 5, "Join flow".
func (s *ClusterService) Join(req pki.JoinRequest) (*pki.JoinResponse, error) {
	if err := s.Start(); err != nil {
		return nil, err
	}
	secret, err := base64.RawURLEncoding.DecodeString(req.Secret)
	if err != nil || len(secret) != 32 {
		return nil, ErrJoinDenied
	}
	csr, err := pki.ParseCSR([]byte(req.CSRPEM))
	if err != nil {
		// Checked before the secret is spent so a malformed request does not
		// burn a valid token... but a bad secret must not be distinguishable
		// from a good one by the answer, so verify the secret *exists* first
		// without consuming it.
		if !s.secretUsable(secret) {
			return nil, ErrJoinDenied
		}
		return nil, fmt.Errorf("%w: %v", ErrBadJoin, err)
	}
	nodeID := csr.Subject.CommonName
	now := s.Now()

	existing, err := s.repo.Nodes.GetNodeByID(nodeID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if existing != nil && existing.Transport == types.TransportLocal {
		if !s.secretUsable(secret) {
			return nil, ErrJoinDenied
		}
		return nil, fmt.Errorf("%w: node id %s belongs to the manager's local agent", ErrBadJoin, nodeID)
	}

	ok, err := s.repo.Cluster.ConsumeJoinToken(pki.HashSecret(secret), now, nodeID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrJoinDenied
	}

	nc, err := s.ca.SignNode(csr, now)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		// Re-enrollment keeps the node id but supersedes every earlier
		// certificate: whoever held the old credential must not keep working.
		if err := s.revokeNodeCerts(nodeID, now); err != nil {
			return nil, err
		}
	}
	if err := s.repo.Nodes.UpsertClusterNode(&types.Node{
		ID:     nodeID,
		Name:   cleanField(req.Node.Name, nodeID),
		IPAddr: cleanField(req.Node.IPAddr, ""),
		OS:     cleanField(req.Node.OS, ""),
		Arch:   cleanField(req.Node.Arch, ""),
	}); err != nil {
		return nil, err
	}
	if err := s.repo.Cluster.RecordNodeCert(nc.Serial, nodeID, now, nc.NotAfter); err != nil {
		return nil, err
	}
	bundle, err := pki.TrustBundle(s.caDir, s.ca)
	if err != nil {
		return nil, err
	}
	return &pki.JoinResponse{NodeID: nodeID, CertPEM: string(nc.PEM), CABundlePEM: string(bundle), NotAfter: nc.NotAfter}, nil
}

// secretUsable reports whether secret is a live, unconsumed token.
func (s *ClusterService) secretUsable(secret []byte) bool {
	rows, err := s.repo.Cluster.ListJoinTokens()
	if err != nil {
		return false
	}
	h := pki.HashSecret(secret)
	for _, t := range rows {
		if t.SecretHash == h && !t.Consumed && t.ExpiresAt.After(s.Now()) {
			return true
		}
	}
	return false
}

// RenewResult is a renewed certificate.
type RenewResult struct {
	CertPEM   string
	BundlePEM string
	NotAfter  time.Time
}

// Renew issues a new certificate to an authenticated member. callerID is the
// CN of the certificate the caller presented over mTLS; the CSR must be for
// that same node. The previous certificate stays valid until it expires (a
// node that crashed between receiving and saving the new one must not be
// locked out); `nodes rm` revokes every certificate the node has.
func (s *ClusterService) Renew(callerID string, csrPEM []byte) (*RenewResult, error) {
	if err := s.Start(); err != nil {
		return nil, err
	}
	csr, err := pki.ParseCSR(csrPEM)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadJoin, err)
	}
	if csr.Subject.CommonName != callerID {
		return nil, fmt.Errorf("%w: a node can only renew its own certificate", ErrNodeForbidden)
	}
	if err := s.CheckMember(callerID); err != nil {
		return nil, err
	}
	now := s.Now()
	nc, err := s.ca.SignNode(csr, now)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Cluster.RecordNodeCert(nc.Serial, callerID, now, nc.NotAfter); err != nil {
		return nil, err
	}
	bundle, err := pki.TrustBundle(s.caDir, s.ca)
	if err != nil {
		return nil, err
	}
	return &RenewResult{CertPEM: string(nc.PEM), BundlePEM: string(bundle), NotAfter: nc.NotAfter}, nil
}

// CheckMember returns nil only if nodeID is a current cluster member.
func (s *ClusterService) CheckMember(nodeID string) error {
	n, err := s.repo.Nodes.GetNodeByID(nodeID)
	if err != nil || n.Transport != types.TransportCluster {
		return fmt.Errorf("%w: %s is not a member of this cluster", ErrNodeForbidden, nodeID)
	}
	return nil
}

// RemoveNode implements `orcli nodes rm`: it revokes every certificate the node
// holds (cluster members), forgets the node and its containers, and tells the
// listener to drop any live connection.
func (s *ClusterService) RemoveNode(nodeID string) (revoked int, err error) {
	if err := s.Start(); err != nil {
		return 0, err
	}
	n, err := s.repo.Nodes.GetNodeByID(nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("no such node %q", nodeID)
		}
		return 0, err
	}
	now := s.Now()
	if n.Transport == types.TransportCluster {
		certs, err := s.repo.Cluster.ListNodeCerts(nodeID)
		if err != nil {
			return 0, err
		}
		revoked = len(certs)
		if err := s.revokeNodeCerts(nodeID, now); err != nil {
			return 0, err
		}
	}
	if err := s.repo.Containers.ReplaceContainersForNode(nodeID, nil); err != nil {
		return revoked, err
	}
	if err := s.repo.Nodes.DeleteNode(nodeID); err != nil {
		return revoked, err
	}
	if s.OnNodeRemoved != nil {
		s.OnNodeRemoved(nodeID)
	}
	return revoked, nil
}

func (s *ClusterService) revokeNodeCerts(nodeID string, now time.Time) error {
	certs, err := s.repo.Cluster.ListNodeCerts(nodeID)
	if err != nil {
		return err
	}
	for _, c := range certs {
		if err := s.repo.Cluster.Revoke(c.Serial, nodeID, c.NotAfter, now); err != nil {
			return err
		}
	}
	if err := s.repo.Cluster.DeleteNodeCerts(nodeID); err != nil {
		return err
	}
	s.mu.Lock()
	err = s.refreshDenyLocked()
	s.mu.Unlock()
	if err == nil && s.OnCertsRevoked != nil {
		s.OnCertsRevoked(nodeID)
	}
	return err
}

// IsLinked reports whether nodeID currently has an agent link.
func (s *ClusterService) IsLinked(nodeID string) bool {
	return s.Linked != nil && s.Linked(nodeID)
}

// IsRevoked reports whether a certificate serial is on the deny list.
func (s *ClusterService) IsRevoked(serial string) bool {
	s.denyMu.RLock()
	defer s.denyMu.RUnlock()
	_, ok := s.deny[serial]
	return ok
}

func (s *ClusterService) refreshDenyLocked() error {
	serials, err := s.repo.Cluster.RevokedSerials(s.Now())
	if err != nil {
		return err
	}
	m := make(map[string]struct{}, len(serials))
	for _, sr := range serials {
		m[sr] = struct{}{}
	}
	s.denyMu.Lock()
	s.deny = m
	s.denyMu.Unlock()
	return nil
}

// PruneRevoked drops deny-list entries whose certificates have expired.
func (s *ClusterService) PruneRevoked() (int64, error) {
	n, err := s.repo.Cluster.PruneRevoked(s.Now())
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return n, s.refreshDenyLocked()
}

// NodeInfo is a node plus its certificate state, for `orcli nodes ls`.
type NodeInfo struct {
	Node     *types.Node
	NotAfter time.Time // zero for local nodes
	Warn     bool      // certificate expires within pki.ExpiryWarning (or already has)
	Expired  bool
}

// ListNodes returns every node with its certificate expiry.
func (s *ClusterService) ListNodes() ([]NodeInfo, error) {
	nodes, _, err := s.repo.Nodes.ListNodes(types.QueryNodesRequest{})
	if err != nil {
		return nil, err
	}
	expiry, err := s.repo.Cluster.LatestNotAfter()
	if err != nil {
		return nil, err
	}
	now := s.Now()
	out := make([]NodeInfo, 0, len(nodes))
	for _, n := range nodes {
		info := NodeInfo{Node: n}
		if n.Transport == types.TransportCluster {
			if na, ok := expiry[n.ID]; ok {
				info.NotAfter = na
				info.Expired = !na.After(now)
				info.Warn = na.Sub(now) < pki.ExpiryWarning
			}
		}
		out = append(out, info)
	}
	return out, nil
}

func cleanField(v, def string) string {
	v = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(v))
	if len(v) > 128 {
		v = v[:128]
	}
	if v == "" {
		return def
	}
	return v
}

// primaryIP finds the address this machine uses to reach the outside world
// (no packet is sent: a UDP "connect" only selects a route).
func primaryIP() (string, error) {
	c, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.String(), nil
}
