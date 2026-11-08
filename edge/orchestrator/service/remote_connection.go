package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/link"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// The standing remote-access connection (RFD 5, Remote access).
//
// The cloud must be able to open a stream toward the cluster's agent at any
// time (Cockpit, `ufctl connect`), which needs a long-lived cloud connection
// that the agent beside the Orchestrator holds open. The Orchestrator is the
// one that talks to the cloud's connection API, so it owns the bookkeeping:
//
//  1. Preconditions. The remote listener is enabled (UNDERLEAF_REMOTE_ADDR) and
//     the cluster is registered with the cloud (the cluster certificate issued
//     at registration exists and loads). Otherwise nothing happens.
//  2. Which node. A link comes up and its agent is the manager-local agent: the
//     link arrived on the local listener (node transport "local", the same
//     flag NodeService.ManagerNode keys on) AND the agent reported in its hello
//     that it has a remote target configured, i.e. it can reach the loopback
//     remote listener. A worker (transport "cluster", no target) never
//     qualifies, and on a cluster with several local-transport agents the first
//     to link holds the connection until it unlinks.
//  3. Ensure. List that node's connections named utils.RemoteConnectionName,
//     reuse a non-closed one, create one only when there is none, then tell the
//     node's agent its id (SetRemoteConnection). A reconnecting agent finds the
//     existing connection, so reconnects never pile up duplicates.
//  4. Resilience. A cloud outage, an agent that answers "not yet" and a cluster
//     that registers later are retried (backoff, or a slow poll for the
//     registration, which costs a file stat). A new link for the node retries
//     at once. A successful ensure is repeated every Refresh, which also heals
//     a connection the cloud closed behind our back.

// errNotOwner means another node holds the standing connection now.
var errNotOwner = errors.New("another node holds the standing connection")

// ErrNotRegistered means the cluster has no cloud credentials yet.
var ErrNotRegistered = errors.New("the cluster is not registered with the cloud")

// CloudConnections is the part of the cloud client the ensurer uses.
type CloudConnections interface {
	ListConnections(ctx context.Context, query types.QueryConnectionsRequest) ([]types.Connection, error)
	CreateConnection(ctx context.Context, req types.CreateConnectionRequest) (*types.Connection, error)
}

var _ CloudConnections = (*clients.CloudClient)(nil)

// RemoteConnectionConfig wires a RemoteConnectionEnsurer. Zero durations take
// production defaults.
type RemoteConnectionConfig struct {
	// Enabled reports whether the remote listener is configured.
	Enabled func() bool
	// Cloud returns a cloud client with the cluster's credentials, or
	// ErrNotRegistered. It is called for every attempt, so a registration made
	// (or a certificate renewed) after the Orchestrator started is picked up.
	Cloud func() (CloudConnections, error)
	// Agents reaches the node's agent.
	Agents clients.AgentDirectory
	// Linked reports whether nodeID has a live link right now.
	Linked func(nodeID string) bool
	Logf   func(format string, args ...any)

	MinBackoff        time.Duration // after a failure; default 2s
	MaxBackoff        time.Duration // cap; default 5m
	NotRegisteredPoll time.Duration // how often to look for a registration; default 30s
	Refresh           time.Duration // re-verify after success; default 10m
	AttemptTimeout    time.Duration // one cloud+agent round; default 30s
}

// RemoteConnectionEnsurer keeps the standing remote-access connection of the
// manager-local node in place. See the comment at the top of this file.
type RemoteConnectionEnsurer struct {
	ctx context.Context
	cfg RemoteConnectionConfig

	mu      sync.Mutex
	owner   string             // node holding the standing connection ("" = none yet)
	workers map[string]*worker // one long-lived goroutine per owner node ever
}

type worker struct {
	nodeID string
	kick   chan struct{} // 1-buffered: "the node (re)linked, try now"
}

// NewRemoteConnectionEnsurer returns an ensurer whose workers stop with ctx.
func NewRemoteConnectionEnsurer(ctx context.Context, cfg RemoteConnectionConfig) *RemoteConnectionEnsurer {
	if cfg.MinBackoff == 0 {
		cfg.MinBackoff = 2 * time.Second
	}
	if cfg.MaxBackoff == 0 {
		cfg.MaxBackoff = 5 * time.Minute
	}
	if cfg.NotRegisteredPoll == 0 {
		cfg.NotRegisteredPoll = 30 * time.Second
	}
	if cfg.Refresh == 0 {
		cfg.Refresh = 10 * time.Minute
	}
	if cfg.AttemptTimeout == 0 {
		cfg.AttemptTimeout = 30 * time.Second
	}
	if cfg.Logf == nil {
		cfg.Logf = func(format string, args ...any) { fmt.Printf("[remote] "+format+"\n", args...) }
	}
	return &RemoteConnectionEnsurer{ctx: ctx, cfg: cfg, workers: map[string]*worker{}}
}

// RemoteAccessEnabled reports whether UNDERLEAF_REMOTE_ADDR is set.
func RemoteAccessEnabled() bool {
	return strings.TrimSpace(os.Getenv(utils.EnvRemoteAddr)) != ""
}

// CloudConnectionsFromCerts builds the Cloud provider used in production: a
// cloud client authenticated with the cluster certificate in cfg.CertDir, or
// ErrNotRegistered while that certificate does not exist (the cluster has not
// completed `orcli cloud register`). Anything else wrong with the credentials
// (unreadable, half-written) is an ordinary error and is retried.
func CloudConnectionsFromCerts(cfg utils.Config) func() (CloudConnections, error) {
	return func() (CloudConnections, error) {
		if !fileExists(filepath.Join(cfg.CertDir, types.CertFileNameClusterCert.String())) {
			return nil, ErrNotRegistered
		}
		cc, err := clients.NewCloudClientWithCert(cfg)
		if err != nil {
			return nil, err
		}
		return cc, nil
	}
}

// Eligible reports whether the agent behind a link can hold the standing
// connection: it is the manager-local agent (local transport) and it can reach
// the remote listener (it has a remote target).
func Eligible(info link.LinkInfo) bool {
	return info.Transport == types.TransportLocal && info.RemoteTarget
}

// NodeLinked is the link observer (link.Hub.OnLinked): a node's link just came
// up. It never blocks.
func (e *RemoteConnectionEnsurer) NodeLinked(info link.LinkInfo) {
	if e.cfg.Enabled == nil || !e.cfg.Enabled() || !Eligible(info) {
		return
	}
	e.mu.Lock()
	if e.owner != "" && e.owner != info.NodeID && e.cfg.Linked != nil && e.cfg.Linked(e.owner) {
		owner := e.owner
		e.mu.Unlock()
		e.cfg.Logf("remote access: node %s also qualifies but %s already holds the standing connection; leaving it there", info.NodeID, owner)
		return
	}
	e.owner = info.NodeID
	w := e.workers[info.NodeID]
	if w == nil {
		// A new worker starts with an attempt of its own; no kick needed.
		w = &worker{nodeID: info.NodeID, kick: make(chan struct{}, 1)}
		e.workers[info.NodeID] = w
		e.mu.Unlock()
		go e.run(w)
		return
	}
	e.mu.Unlock()
	select {
	case w.kick <- struct{}{}:
	default: // a kick is already pending
	}
}

func (e *RemoteConnectionEnsurer) isOwner(nodeID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.owner == nodeID
}

// run is the node's loop: attempt, then wait for the retry timer or a kick.
func (e *RemoteConnectionEnsurer) run(w *worker) {
	backoff := e.cfg.MinBackoff
	notRegisteredLogged := false
	lastID := ""
	for {
		id, err := e.attempt(w.nodeID)
		var wait <-chan time.Time // nil = wait for a kick only
		switch {
		case err == nil:
			backoff = e.cfg.MinBackoff
			notRegisteredLogged = false
			if id != lastID {
				e.cfg.Logf("remote access: node %s holds standing connection %s (%s)", w.nodeID, id, utils.RemoteConnectionName)
				lastID = id
			}
			wait = time.After(e.cfg.Refresh)
		case errors.Is(err, ErrNotRegistered):
			if !notRegisteredLogged {
				e.cfg.Logf("remote access: this cluster is not registered with the cloud yet; no standing connection until it is (will check again)")
				notRegisteredLogged = true
			}
			wait = time.After(e.cfg.NotRegisteredPoll)
		case errors.Is(err, errNotOwner):
			// Stand down until this node is made the owner again (a kick).
		case isNoLink(err):
			// The link is gone; its successor kicks us.
			e.cfg.Logf("remote access: node %s is not linked; waiting for it", w.nodeID)
		default:
			e.cfg.Logf("remote access: could not ensure the standing connection for node %s (retrying in %s): %v", w.nodeID, backoff, err)
			wait = time.After(backoff)
			backoff = min(backoff*2, e.cfg.MaxBackoff)
		}
		select {
		case <-e.ctx.Done():
			return
		case <-w.kick:
			backoff = e.cfg.MinBackoff
		case <-wait:
		}
	}
}

func isNoLink(err error) bool {
	var nl *clients.NoLinkError
	return errors.As(err, &nl) || errors.Is(err, link.ErrLinkDropped)
}

// attempt runs one ensure round for nodeID and returns the connection id it
// settled on.
func (e *RemoteConnectionEnsurer) attempt(nodeID string) (string, error) {
	if !e.isOwner(nodeID) {
		return "", errNotOwner
	}
	cloud, err := e.cfg.Cloud()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(e.ctx, e.cfg.AttemptTimeout)
	defer cancel()

	conn, err := findOrCreateRemoteConnection(ctx, cloud, nodeID)
	if err != nil {
		return "", err
	}
	if err := e.cfg.Agents.Agent(nodeID).SetRemoteConnection(ctx, conn.ID); err != nil {
		return "", fmt.Errorf("tell node %s its remote connection: %w", nodeID, err)
	}
	return conn.ID, nil
}

// findOrCreateRemoteConnection returns the node's live "remote-access"
// connection, creating one only if the cloud has none that is not closed.
func findOrCreateRemoteConnection(ctx context.Context, cloud CloudConnections, nodeID string) (*types.Connection, error) {
	name := utils.RemoteConnectionName
	conns, err := cloud.ListConnections(ctx, types.QueryConnectionsRequest{NodeID: &nodeID, Name: &name})
	if err != nil {
		return nil, err
	}
	if c := pickRemoteConnection(conns, nodeID); c != nil {
		return c, nil
	}
	conn, err := cloud.CreateConnection(ctx, types.CreateConnectionRequest{NodeID: nodeID, Name: name})
	if err != nil {
		return nil, err
	}
	if conn == nil || conn.ID == "" {
		return nil, errors.New("the cloud created a connection without an id")
	}
	return conn, nil
}

// pickRemoteConnection chooses among the connections the cloud listed. It
// re-checks name and node (the cloud's filter is not trusted to be exact),
// skips closed ones, prefers a running connection over a merely provisioned
// one and the newest among equals.
func pickRemoteConnection(conns []types.Connection, nodeID string) *types.Connection {
	rank := func(c types.Connection) int {
		if c.State == types.ConnectionStateRunning {
			return 2
		}
		return 1
	}
	var best *types.Connection
	for i := range conns {
		c := conns[i]
		if c.Name != utils.RemoteConnectionName || string(c.NodeID) != nodeID ||
			c.State == types.ConnectionStateClosed || c.ClosedAt != nil || c.ID == "" {
			continue
		}
		if best == nil || rank(c) > rank(*best) || (rank(c) == rank(*best) && c.CreatedAt > best.CreatedAt) {
			best = &conns[i]
		}
	}
	return best
}
