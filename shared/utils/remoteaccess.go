package utils

import (
	"net"
	"os"
	"strconv"
	"strings"
)

// Remote access configuration (RFD 5, step 7b). Remote access is off unless
// the operator sets these.
const (
	// EnvRemoteAddr is host:port the Orchestrator's remote listener binds
	// (assertion required on every request). Unset = no remote listener.
	EnvRemoteAddr = "UNDERLEAF_REMOTE_ADDR"
	// EnvRemoteTarget is host:port the agent connects cloud-opened remote
	// streams to: the Orchestrator's remote listener as the agent reaches it.
	// Unset = the agent refuses cloud-opened streams.
	EnvRemoteTarget = "UNDERLEAF_REMOTE_TARGET"
	// EnvRemoteConnectionID is the cloud connection id the agent keeps open so
	// the cloud can open remote streams toward it. Optional: without it the
	// agent only accepts remote streams on a connection that gateway streams
	// opened.
	EnvRemoteConnectionID = "UNDERLEAF_REMOTE_CONNECTION_ID"

	// DefaultRemotePort is the conventional remote listener port. It is always
	// on the stream denylist, configured or not.
	DefaultRemotePort NamedPort = 50110
)

// remotePorts returns the remote-access ports that gateway streams must never
// target, keyed like ControlPlanePorts. The remote listener demands an
// assertion, so a stream to it would fail closed anyway; this is the second
// wall.
func remotePorts() map[int]string {
	ports := map[int]string{DefaultRemotePort.Int(): "Orchestrator remote listener"}
	for _, env := range []string{EnvRemoteAddr, EnvRemoteTarget} {
		v := strings.TrimSpace(os.Getenv(env))
		if v == "" {
			continue
		}
		if _, port, err := net.SplitHostPort(v); err == nil {
			if p, err := strconv.Atoi(port); err == nil && p > 0 && p <= 65535 {
				ports[p] = "Orchestrator remote listener (" + env + ")"
			}
		}
	}
	return ports
}
