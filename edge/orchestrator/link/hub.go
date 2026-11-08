// Package link is the Orchestrator side of the agent link (RFD 5, decisions 5
// and 6). Agents dial out and keep one bidirectional gRPC stream open; this
// package holds the set of live links, routes commands down them and matches the
// answers that come back up by request id. The Orchestrator never initiates a
// connection to an agent and has no address for one.
package link

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentpb "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// ErrLinkDropped is wrapped by the error returned to a caller whose command was
// in flight when the link went away: the command may or may not have run.
var ErrLinkDropped = errors.New("agent link dropped while the command was in flight (its outcome is unknown)")

// Hub is the registry of live agent links. It implements clients.AgentDirectory.
type Hub struct {
	mu       sync.Mutex
	sessions map[string]*session // node id -> live link
	streams  map[string]*logStream
	onLinked func(LinkInfo)
}

// LinkInfo describes one live link: who it is and what its agent said about
// itself in the hello.
type LinkInfo struct {
	NodeID string
	// Transport is "local" (the manager's own agent, on the loopback listener)
	// or "cluster" (a worker, over mTLS).
	Transport string
	// RemoteTarget is true when the agent has a remote-access target
	// configured, i.e. it can reach the Orchestrator's remote listener.
	RemoteTarget bool
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{sessions: map[string]*session{}, streams: map[string]*logStream{}}
}

var _ clients.AgentDirectory = (*Hub)(nil)

// session is one live link.
type session struct {
	nodeID    string
	transport string
	remote    bool // the agent reported a remote-access target in its hello
	send      func(*agentpb.OrchestratorMessage) error
	sendMu    sync.Mutex

	mu      sync.Mutex
	pending map[string]chan *agentpb.CommandResult
	closed  chan struct{}
	once    sync.Once
	reason  error
}

func newSession(nodeID, transport string, send func(*agentpb.OrchestratorMessage) error) *session {
	return &session{
		nodeID:    nodeID,
		transport: transport,
		send:      send,
		pending:   map[string]chan *agentpb.CommandResult{},
		closed:    make(chan struct{}),
	}
}

// close ends the session; in-flight callers are released with ErrLinkDropped.
func (s *session) close(reason error) {
	s.once.Do(func() {
		s.reason = reason
		close(s.closed)
	})
}

func (s *session) sendMsg(m *agentpb.OrchestratorMessage) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.send(m)
}

// deliver hands an answer to the caller waiting for it. An answer nobody waits
// for (the caller timed out or was cancelled) is dropped.
func (s *session) deliver(res *agentpb.CommandResult) bool {
	s.mu.Lock()
	ch, ok := s.pending[res.GetRequestId()]
	if ok {
		delete(s.pending, res.GetRequestId())
	}
	s.mu.Unlock()
	if ok {
		ch <- res // buffered, capacity 1
	}
	return ok
}

func (s *session) call(ctx context.Context, cmd *agentpb.Command) (*agentpb.CommandResult, error) {
	cmd.RequestId = types.GenerateID(types.RequestIDPrefix)
	ch := make(chan *agentpb.CommandResult, 1)

	s.mu.Lock()
	select {
	case <-s.closed:
		s.mu.Unlock()
		return nil, &clients.NoLinkError{NodeID: s.nodeID}
	default:
	}
	s.pending[cmd.RequestId] = ch
	s.mu.Unlock()
	forget := func() {
		s.mu.Lock()
		delete(s.pending, cmd.RequestId)
		s.mu.Unlock()
	}

	if err := s.sendMsg(&agentpb.OrchestratorMessage{Msg: &agentpb.OrchestratorMessage_Command{Command: cmd}}); err != nil {
		forget()
		return nil, fmt.Errorf("send command to node %s: %w", s.nodeID, err)
	}
	select {
	case res := <-ch:
		return res, nil
	case <-ctx.Done():
		forget()
		// Tell the agent to stop working on it; best effort.
		_ = s.sendMsg(&agentpb.OrchestratorMessage{Msg: &agentpb.OrchestratorMessage_Cancel{Cancel: &agentpb.Cancel{RequestId: cmd.RequestId}}})
		return nil, ctx.Err()
	case <-s.closed:
		forget()
		return nil, fmt.Errorf("node %s: %w", s.nodeID, ErrLinkDropped)
	}
}

// register installs s as the live link for its node. A node has at most one
// link: a new one supersedes the old (the old agent connection may be half
// dead; a reconnecting agent must not be locked out by its own ghost).
func (h *Hub) register(s *session) {
	h.mu.Lock()
	old := h.sessions[s.nodeID]
	h.sessions[s.nodeID] = s
	h.mu.Unlock()
	if old != nil {
		old.close(errors.New("superseded by a newer link from the same node"))
	}
}

func (h *Hub) unregister(s *session) {
	h.mu.Lock()
	if h.sessions[s.nodeID] == s {
		delete(h.sessions, s.nodeID)
	}
	h.mu.Unlock()
	s.close(errors.New("link closed"))
}

// Drop closes the link of a node (e.g. after `nodes rm`); the agent's stream
// ends and, being revoked, it cannot reconnect.
func (h *Hub) Drop(nodeID string) {
	h.mu.Lock()
	s := h.sessions[nodeID]
	h.mu.Unlock()
	if s != nil {
		s.close(errors.New("link dropped by the Orchestrator"))
	}
}

// OnLinked registers fn to be called (in its own goroutine) each time a link
// has been accepted and welcomed. There is at most one observer; a later call
// replaces it. Call it before the listeners start.
func (h *Hub) OnLinked(fn func(LinkInfo)) {
	h.mu.Lock()
	h.onLinked = fn
	h.mu.Unlock()
}

func (h *Hub) notifyLinked(s *session) {
	h.mu.Lock()
	fn := h.onLinked
	h.mu.Unlock()
	if fn != nil {
		go fn(LinkInfo{NodeID: s.nodeID, Transport: s.transport, RemoteTarget: s.remote})
	}
}

// Links describes every live link, sorted by node id.
func (h *Hub) Links() []LinkInfo {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]LinkInfo, 0, len(h.sessions))
	for _, s := range h.sessions {
		out = append(out, LinkInfo{NodeID: s.nodeID, Transport: s.transport, RemoteTarget: s.remote})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out
}

// Linked reports whether nodeID has a live link.
func (h *Hub) Linked(nodeID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.sessions[nodeID]
	return ok
}

// LinkedNodes lists the node ids with a live link, sorted.
func (h *Hub) LinkedNodes() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	ids := make([]string, 0, len(h.sessions))
	for id := range h.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Agent implements clients.AgentDirectory.
func (h *Hub) Agent(nodeID string) clients.AgentAPI { return &nodeClient{hub: h, nodeID: nodeID} }

// Call sends one command to nodeID and waits for the answer matching its
// request id. It fails fast with *clients.NoLinkError if the node has no link.
func (h *Hub) Call(ctx context.Context, nodeID string, cmd *agentpb.Command) (*agentpb.CommandResult, error) {
	h.mu.Lock()
	s := h.sessions[nodeID]
	h.mu.Unlock()
	if s == nil {
		return nil, &clients.NoLinkError{NodeID: nodeID}
	}
	res, err := s.call(ctx, cmd)
	if err != nil {
		return nil, err
	}
	if e := res.GetError(); e != nil {
		return nil, status.Error(codes.Code(e.GetCode()), e.GetMessage())
	}
	return res, nil
}

// --- log sub-streams -----------------------------------------------------------

type logStream struct {
	nodeID   string
	lines    chan *agentpb.LogLine
	attached chan struct{}
	done     chan struct{}
	once     sync.Once
	closeOne sync.Once
}

// cancel tells the sub-stream handler to stop.
func (l *logStream) cancel() { l.once.Do(func() { close(l.done) }) }

// closeLines ends the consumer's channel exactly once.
func (l *logStream) closeLines() { l.closeOne.Do(func() { close(l.lines) }) }

func newToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return hex.EncodeToString(b)
}

// expectStream registers a capability for a sub-stream the agent is about to open.
func (h *Hub) expectStream(nodeID string) (string, *logStream) {
	tok := newToken()
	ls := &logStream{nodeID: nodeID, lines: make(chan *agentpb.LogLine, 256), attached: make(chan struct{}), done: make(chan struct{})}
	h.mu.Lock()
	h.streams[tok] = ls
	h.mu.Unlock()
	return tok, ls
}

func (h *Hub) forgetStream(tok string) {
	h.mu.Lock()
	delete(h.streams, tok)
	h.mu.Unlock()
}

// attachStream is called by the LogStream handler: it succeeds only for a live
// token that was issued for exactly this (authenticated) node, and only once.
func (h *Hub) attachStream(tok, nodeID string) (*logStream, error) {
	h.mu.Lock()
	ls, ok := h.streams[tok]
	if ok && ls.nodeID == nodeID {
		delete(h.streams, tok) // single use
	} else {
		ok = false
	}
	h.mu.Unlock()
	if !ok {
		return nil, status.Error(codes.PermissionDenied, "unknown or foreign log stream token")
	}
	close(ls.attached)
	return ls, nil
}

const streamAttachTimeout = 15 * time.Second
