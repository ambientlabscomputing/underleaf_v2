package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/pki"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// JoinOptions are the inputs of JoinCluster that are not the join string.
type JoinOptions struct {
	// StateDir is where membership state is stored (utils.ClusterStateDir).
	StateDir string
	// Force allows re-pointing a node whose current certificate is still valid
	// at a different cluster.
	Force bool
	// Host reports this machine's details to the manager. Optional.
	Host func() (*HostInfo, error)
	// SaveNode records the node in the agent's own database. Optional.
	SaveNode func(*types.Node) error
	// Now is injectable for tests.
	Now func() time.Time
}

// JoinOutcome describes a completed enrollment.
type JoinOutcome struct {
	NodeID     string
	ReEnrolled bool // the node id was kept from an earlier enrollment
	NotAfter   time.Time
	Member     *pki.Member
}

// JoinCluster enrolls this machine into the cluster named by joinString (RFD 5,
// "Join flow"). The manager is authenticated against the fingerprint in the
// string before the secret is sent; see pki.Join. Re-running it with a fresh
// string keeps the node id, which is the backstop for a node whose certificate
// expired while it was offline.
func JoinCluster(ctx context.Context, joinString string, opts JoinOptions) (*JoinOutcome, error) {
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	info, err := pki.ParseJoinString(joinString)
	if err != nil {
		return nil, err
	}
	if now().After(info.ExpiresAt) {
		return nil, fmt.Errorf("this join string expired at %s: ask for a fresh one with `orcli nodes add` on the manager", info.ExpiresAt.Format(time.RFC3339))
	}

	nodeID := ""
	reEnroll := false
	switch prev, err := pki.LoadMember(opts.StateDir); {
	case err == nil:
		nodeID, reEnroll = prev.State.NodeID, true
		sameCluster := prev.State.CAFingerprint == pki.FormatFingerprint(info.Fingerprint)
		if !sameCluster && prev.NotAfter().After(now()) && !opts.Force {
			return nil, fmt.Errorf("this node is already a member of another cluster (CA %s) with a certificate valid until %s; pass --force to leave it and join this one", prev.State.CAFingerprint, prev.NotAfter().Format("2006-01-02"))
		}
	case errors.Is(err, pki.ErrNotMember):
		nodeID = types.GenerateID(types.NodeIDPrefix)
	default:
		return nil, fmt.Errorf("existing membership state in %s is unreadable (%v); remove that directory to start clean", opts.StateDir, err)
	}

	node := pki.JoinNode{}
	if opts.Host != nil {
		if h, err := opts.Host(); err == nil {
			node = pki.JoinNode{Name: h.Hostname, IPAddr: h.IPAddr, OS: h.OS, Arch: h.Arch}
		}
	}
	if node.Name == "" {
		node.Name, _ = os.Hostname()
	}

	res, err := pki.Join(ctx, info, nodeID, node)
	if err != nil {
		return nil, err
	}
	member, err := pki.SaveMember(opts.StateDir, pki.MemberState{
		ManagerAddr:   info.Addr,
		NodeID:        nodeID,
		CAFingerprint: pki.FormatFingerprint(info.Fingerprint),
		JoinedAt:      now(),
	}, res.Key, res.CertPEM, res.Bundle)
	if err != nil {
		return nil, fmt.Errorf("joined, but could not save credentials: %w", err)
	}
	if opts.SaveNode != nil {
		if err := opts.SaveNode(&types.Node{ID: nodeID, Name: node.Name, IPAddr: node.IPAddr, OS: node.OS, Arch: node.Arch}); err != nil {
			return nil, fmt.Errorf("joined, but could not record the node locally: %w", err)
		}
	}
	return &JoinOutcome{NodeID: nodeID, ReEnrolled: reEnroll, NotAfter: res.NotAfter, Member: member}, nil
}

// VerifyMembership proves the new credentials work: it reconnects to the
// manager with mTLS (the last step of the join sequence) and pings it.
func VerifyMembership(ctx context.Context, m *pki.Member) error {
	conn, err := clients.DialCluster(m)
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	oc := clients.NewOrchestratorClientFromConn(conn, m)
	_, err = oc.Ping(ctx, m.State.NodeID)
	return err
}
