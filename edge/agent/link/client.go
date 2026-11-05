// Package link is the agent side of the agent link (RFD 5, decisions 5 and 6).
//
// The agent dials OUT to the Orchestrator and keeps one long-lived gRPC
// bidirectional stream open. The Orchestrator sends commands down it; the agent
// runs them and sends the results back up, matched by request id. A worker
// therefore listens on nothing. If the stream drops, the client reconnects with
// exponential backoff, forever.
package link

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	agentpb "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
)

// Target is one established connection to the Orchestrator.
type Target struct {
	Conn   *grpc.ClientConn
	NodeID string
	// Local is true on the loopback listener, where there is no certificate
	// and the agent must name its node itself.
	Local bool
}

// Env is what a Handler needs besides the command.
type Env struct {
	Conn   grpc.ClientConnInterface
	NodeID string
	Local  bool
	// LinkCtx lives as long as this link; background work started by a command
	// (a log sub-stream) is tied to it.
	LinkCtx context.Context
}

// Handler runs one command. It must return a result carrying cmd's request id.
type Handler interface {
	Handle(ctx context.Context, cmd *agentpb.Command, env Env) *agentpb.CommandResult
}

// Client maintains the agent link.
type Client struct {
	// Dial opens a fresh connection each time the link is (re)established, so
	// membership changes (a node that just joined a cluster) are picked up
	// without a restart.
	Dial    func(ctx context.Context) (*Target, error)
	Handler Handler
	Version string

	MinBackoff  time.Duration // default 500ms
	MaxBackoff  time.Duration // default 30s
	StableAfter time.Duration // a link this old resets the backoff; default 15s
	MaxInFlight int           // concurrent commands; default 64
	Logf        func(format string, args ...any)
	// OnState is called with true once the link is welcome and false when it
	// ends. Optional.
	OnState func(up bool)
}

func (c *Client) defaults() {
	if c.MinBackoff == 0 {
		c.MinBackoff = 500 * time.Millisecond
	}
	if c.MaxBackoff == 0 {
		c.MaxBackoff = 30 * time.Second
	}
	if c.StableAfter == 0 {
		c.StableAfter = 15 * time.Second
	}
	if c.MaxInFlight == 0 {
		c.MaxInFlight = 64
	}
	if c.Logf == nil {
		c.Logf = func(string, ...any) {}
	}
}

// Run keeps the link up until ctx is cancelled.
func (c *Client) Run(ctx context.Context) {
	c.defaults()
	backoff := c.MinBackoff
	for ctx.Err() == nil {
		started := time.Now()
		err := c.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) >= c.StableAfter {
			backoff = c.MinBackoff
		}
		c.Logf("[link] disconnected (%v); reconnecting in %s", err, backoff.Round(time.Millisecond))
		select {
		case <-ctx.Done():
			return
		case <-time.After(jitter(backoff)):
		}
		backoff = min(backoff*2, c.MaxBackoff)
	}
}

// jitter spreads reconnects of many agents (e.g. after an Orchestrator
// restart) by +-20%.
func jitter(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64()))
}

type flight struct {
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
}

func (f *flight) add(id string, cancel context.CancelFunc) {
	f.mu.Lock()
	f.cancel[id] = cancel
	f.mu.Unlock()
}
func (f *flight) done(id string) {
	f.mu.Lock()
	delete(f.cancel, id)
	f.mu.Unlock()
}
func (f *flight) abort(id string) {
	f.mu.Lock()
	cancel := f.cancel[id]
	f.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *Client) runOnce(ctx context.Context) (err error) {
	t, err := c.Dial(ctx)
	if err != nil {
		return err
	}
	defer t.Conn.Close()

	lctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := agentpb.NewAgentLinkClient(t.Conn).Link(lctx)
	if err != nil {
		return err
	}
	hello := &agentpb.LinkHello{AgentVersion: c.Version}
	if t.Local {
		hello.NodeId = t.NodeID
	}
	if err := stream.Send(&agentpb.AgentMessage{Msg: &agentpb.AgentMessage_Hello{Hello: hello}}); err != nil {
		return err
	}

	// The Orchestrator answers the hello with a welcome, or ends the stream with
	// the reason it refused us.
	welcomeTimer := time.AfterFunc(15*time.Second, cancel)
	first, err := stream.Recv()
	welcomeTimer.Stop()
	if err != nil {
		return explain(err)
	}
	w := first.GetWelcome()
	if w == nil {
		return errors.New("the Orchestrator's first message was not a welcome")
	}
	c.Logf("[link] linked to the Orchestrator as %s (%s)", w.GetNodeId(), w.GetTransport())
	if c.OnState != nil {
		c.OnState(true)
		defer c.OnState(false)
	}

	env := Env{Conn: t.Conn, NodeID: w.GetNodeId(), Local: t.Local, LinkCtx: lctx}
	fl := &flight{cancel: map[string]context.CancelFunc{}}
	sem := make(chan struct{}, c.MaxInFlight)
	var sendMu sync.Mutex
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel() // runs first: abort every in-flight command before waiting

	for {
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return errors.New("the Orchestrator closed the link")
			}
			return explain(err)
		}
		switch m := msg.GetMsg().(type) {
		case *agentpb.OrchestratorMessage_Command:
			cmd := m.Command
			select {
			case sem <- struct{}{}: // backpressure: stop reading when saturated
			case <-lctx.Done():
				return lctx.Err()
			}
			cctx, ccancel := context.WithCancel(lctx)
			fl.add(cmd.GetRequestId(), ccancel)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				defer ccancel()
				defer fl.done(cmd.GetRequestId())
				res := c.Handler.Handle(cctx, cmd, env)
				if res == nil {
					res = errResult(cmd.GetRequestId(), status.Error(codes.Internal, "agent produced no result"))
				}
				res.RequestId = cmd.GetRequestId()
				sendMu.Lock()
				err := stream.Send(&agentpb.AgentMessage{Msg: &agentpb.AgentMessage_Result{Result: res}})
				sendMu.Unlock()
				if err != nil {
					cancel() // the link is broken; tear it down so we reconnect
				}
			}()
		case *agentpb.OrchestratorMessage_Cancel:
			fl.abort(m.Cancel.GetRequestId())
		}
	}
}

// explain turns a refusal from the Orchestrator into something an operator can act on.
func explain(err error) error {
	if s, ok := status.FromError(err); ok {
		switch s.Code() {
		case codes.PermissionDenied, codes.Unauthenticated, codes.NotFound, codes.InvalidArgument:
			return fmt.Errorf("the Orchestrator refused the link: %s", s.Message())
		}
	}
	return err
}

// errResult builds a failed CommandResult from err, keeping its gRPC code.
func errResult(requestID string, err error) *agentpb.CommandResult {
	st, _ := status.FromError(err)
	code := st.Code()
	if code == codes.OK {
		code = codes.Unknown
	}
	return &agentpb.CommandResult{
		RequestId: requestID,
		Error:     &agentpb.CommandError{Code: int32(code), Message: st.Message()},
	}
}
