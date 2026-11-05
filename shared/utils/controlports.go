package utils

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Control-plane port denylist (RFD 5, step 2).
//
// Gateway streams publish a local TCP port to the public internet. Loopback is
// not a trust boundary when something on the host forwards to it, so a stream
// must never target a port that serves the edge control plane (Orchestrator
// REST/gRPC, agent REST/gRPC, the Docker daemon API). This file is the one
// place that set is defined; both the Orchestrator (at stream creation) and the
// agent (where the stream is opened and forwarded) consult it.
//
// The set honours configured ports, not only the defaults: the REST ports come
// from the process's config (UNDERLEAF_CONFIG), and operators who publish the
// control plane on other host ports list them in EnvExtraControlPorts. The
// agent check is the authoritative one, because only the agent process knows
// the agent's own configuration.

// EnvExtraControlPorts is a comma-separated list of additional TCP ports that
// streams may never target (e.g. non-default published Orchestrator ports).
const EnvExtraControlPorts = "UNDERLEAF_EXTRA_CONTROL_PORTS"

const (
	// OrchestratorPublicGRPCPort is where the Orchestrator's agent-facing gRPC
	// listener actually binds (orchestrator/interface/grpc_public/server).
	OrchestratorPublicGRPCPort NamedPort = 50100
	// AgentPublicGRPCPort is where the agent's gRPC listener actually binds
	// (agent/interface/grpc_public/server).
	AgentPublicGRPCPort NamedPort = 50101

	dockerAPIPlainPort NamedPort = 2375
	dockerAPITLSPort   NamedPort = 2376
)

// ErrControlPlanePort is returned (wrapped) for streams that target a
// control-plane port or an invalid port.
var ErrControlPlanePort = errors.New("stream target port is not allowed")

// ControlPlanePorts returns the denied TCP ports mapped to a description of
// what they are. It is recomputed on each call so configuration changes are
// honoured.
func ControlPlanePorts() map[int]string {
	ports := map[int]string{
		OrchestratorHttpPort.Int():       "Orchestrator REST API",
		OrchestratorPublicGRPCPort.Int(): "Orchestrator gRPC",
		OrchestratorGRPCPort.Int():       "Orchestrator gRPC (configured constant)",
		AgentHttpPort.Int():              "agent REST API",
		AgentPublicGRPCPort.Int():        "agent gRPC",
		AgentGRPCPort.Int():              "agent gRPC (configured constant)",
		dockerAPIPlainPort.Int():         "Docker daemon API",
		dockerAPITLSPort.Int():           "Docker daemon API (TLS)",
	}
	// Configured listeners. Both configs are consulted in every process: the
	// config file belongs to this process, but denying one extra port is
	// harmless and a missing one is not.
	if p := GetConfig(OrchestratorConfig).Http.Port.Int(); p > 0 {
		ports[p] = "Orchestrator REST API (configured)"
	}
	if p := GetConfig(AgentConfig).Http.Port.Int(); p > 0 {
		ports[p] = "agent REST API (configured)"
	}
	for _, f := range strings.Split(os.Getenv(EnvExtraControlPorts), ",") {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		if p, err := strconv.Atoi(f); err == nil && p > 0 && p <= 65535 {
			ports[p] = "operator-configured control-plane port (" + EnvExtraControlPorts + ")"
		}
	}
	return ports
}

// CheckStreamPort returns nil if a gateway stream may target the local TCP
// port, or an error wrapping ErrControlPlanePort if not.
func CheckStreamPort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("%w: %d is not a valid TCP port", ErrControlPlanePort, port)
	}
	if what, denied := ControlPlanePorts()[port]; denied {
		return fmt.Errorf("%w: port %d serves the %s and cannot be published as a stream", ErrControlPlanePort, port, what)
	}
	return nil
}
