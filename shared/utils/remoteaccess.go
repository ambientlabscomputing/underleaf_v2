package utils

import (
	"net"
	"os"
	"strconv"
	"strings"
)

// Remote access configuration (RFD 5, step 7b). Remote access is off unless
// the operator sets these. install.sh sets the first two on the manager host
// (UNDERLEAF_REMOTE_ADDR for orch-server, UNDERLEAF_REMOTE_TARGET for ufagentd,
// both 127.0.0.1:50110) and `make run` sets them for the dev server and agent;
// any other way of launching the binaries leaves remote access off. A worker
// installed with `install.sh join` never gets a target: its loopback has no
// remote listener.
const (
	// EnvRemoteAddr is host:port the Orchestrator's remote listener binds
	// (assertion required on every request). Unset = no remote listener.
	EnvRemoteAddr = "UNDERLEAF_REMOTE_ADDR"
	// EnvRemoteTarget is host:port the agent connects cloud-opened remote
	// streams to: the Orchestrator's remote listener as the agent reaches it.
	// Unset = the agent refuses cloud-opened streams. An agent that has it set
	// says so in its link hello, which is what makes it a candidate to hold the
	// cluster's standing remote-access connection.
	EnvRemoteTarget = "UNDERLEAF_REMOTE_TARGET"
	// EnvRemoteConnectionID is an optional manual override of the cloud
	// connection id the agent holds open for remote access. Normally the
	// Orchestrator finds or creates the cluster's "remote-access" connection
	// and tells the agent its id over the agent link; when this is set the agent
	// uses it instead and ignores the Orchestrator's.
	EnvRemoteConnectionID = "UNDERLEAF_REMOTE_CONNECTION_ID"

	// RemoteConnectionName is the name of the cluster's standing cloud
	// connection. It is a contract with the cloud, which finds the connection
	// to open a remote-access stream on by this name, the node being in the
	// cluster, and the connection being running.
	RemoteConnectionName = "remote-access"

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

// RemoteTargetFromEnv returns UNDERLEAF_REMOTE_TARGET, trimmed ("" = remote
// access is not configured on this agent).
func RemoteTargetFromEnv() string {
	return strings.TrimSpace(os.Getenv(EnvRemoteTarget))
}
