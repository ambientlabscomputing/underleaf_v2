package conn_client

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/ambientlabscomputing/underleaf_v2/shared/assertion"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// Remote access over the tunnel (RFD 5, step 7b), agent half.
//
// Gateway streams are opened BY the agent and bound to a local port the agent
// chose. Remote-access streams run the other way: the cloud opens a yamux
// stream toward the agent on the same outbound connection, writes
// assertion.StreamPreamble, and then speaks HTTP, each request carrying a
// cloud-signed assertion header. The agent splices such a stream to exactly one
// address, the Orchestrator's remote listener (UNDERLEAF_REMOTE_TARGET).
//
// Why this keeps remote traffic from being confused with gateway traffic:
//   - the two use opposite stream directions, and the agent never accepts a
//     stream that does not start with the exact preamble;
//   - the destination is configuration, not something the stream names, so a
//     stream cannot be steered to any other local port;
//   - the remote listener refuses every request without a valid assertion, and
//     its port is on the gateway stream denylist (utils.CheckStreamPort), so a
//     public gateway stream cannot be pointed at it either;
//   - with no target configured the agent closes every cloud-opened stream.

// preambleTimeout is how long a cloud-opened stream may take to send its
// preamble before the agent drops it (a variable only so tests can shorten it).
var preambleTimeout = 5 * time.Second

const (
	dialTimeout = 5 * time.Second
	// maxRemoteStreams bounds concurrently open cloud-opened streams.
	maxRemoteStreams = 128
)

// SetRemoteTarget sets the host:port cloud-opened remote streams are connected
// to. Empty disables remote access (all such streams are refused).
func (cc *ConnClient) SetRemoteTarget(addr string) {
	cc.mu.Lock()
	cc.remoteTarget = addr
	cc.mu.Unlock()
}

func (cc *ConnClient) remoteTargetAddr() string {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.remoteTarget
}

// ServeRemoteStreams accepts streams the cloud opens on sess until the session
// ends or ctx is done. target is consulted for every stream; "" refuses it.
func ServeRemoteStreams(ctx context.Context, sess *yamux.Session, target func() string) {
	logger := utils.LoggerFromContext(ctx)
	sem := make(chan struct{}, maxRemoteStreams)
	for {
		stream, err := sess.AcceptStream()
		if err != nil {
			return // session closed
		}
		select {
		case sem <- struct{}{}:
		default:
			logger.Warn("too many remote streams, refusing one")
			stream.Close()
			continue
		}
		go func() {
			defer func() { <-sem }()
			handleRemoteStream(ctx, stream, target())
		}()
	}
}

func handleRemoteStream(ctx context.Context, stream net.Conn, target string) {
	logger := utils.LoggerFromContext(ctx)
	if target == "" {
		logger.Warn("refusing cloud-opened stream: remote access is not configured on this agent")
		stream.Close()
		return
	}
	_ = stream.SetReadDeadline(time.Now().Add(preambleTimeout))
	buf := make([]byte, len(assertion.StreamPreamble))
	if _, err := io.ReadFull(stream, buf); err != nil || string(buf) != assertion.StreamPreamble {
		logger.Warn("refusing cloud-opened stream: bad preamble")
		stream.Close()
		return
	}
	_ = stream.SetReadDeadline(time.Time{})

	upstream, err := (&net.Dialer{Timeout: dialTimeout}).DialContext(ctx, "tcp", target)
	if err != nil {
		logger.Error("remote listener unreachable", "target", target, "error", err)
		stream.Close()
		return
	}
	spliceStreams(ctx, upstream, stream, func() {})
}
