package utils

import (
	"os"
	"path/filepath"
	"strings"
)

// Endpoint locations shared by the orchestrator, agent and CLIs. Every one has
// an environment override so a second instance (a test, a scratch cluster, a
// developer's second checkout) can run beside a real install without touching
// its sockets or ports.
const (
	// EnvOrchLocalPort is the TCP port of the Orchestrator's local (loopback,
	// unauthenticated) gRPC listener. Default 50100.
	EnvOrchLocalPort = "UNDERLEAF_ORCH_LOCAL_PORT"
	// EnvOrchClusterPort is the TCP port of the cluster listener (mTLS). Default 50104.
	EnvOrchClusterPort = "UNDERLEAF_ORCH_CLUSTER_PORT"
	// EnvClusterBind is the interface the cluster listener binds. Default
	// 0.0.0.0: that is safe because the listener cannot exist without mTLS.
	EnvClusterBind = "UNDERLEAF_CLUSTER_BIND"
	// EnvOrchestratorAddr is where an agent on the manager host (single-node,
	// local listener) dials the Orchestrator. Default 127.0.0.1:50100.
	EnvOrchestratorAddr = "UNDERLEAF_ORCHESTRATOR_ADDR"
	// EnvOrchSocket and EnvAgentSocket override the CLI unix sockets.
	EnvOrchSocket  = "UNDERLEAF_ORCH_SOCKET"
	EnvAgentSocket = "UNDERLEAF_AGENT_SOCKET"
	// EnvStateDir overrides where cluster membership state lives (the
	// manager's CA, or a worker's node credentials). See ClusterStateDir.
	EnvStateDir = "UNDERLEAF_CLUSTER_DIR"

	defaultOrchLocalPort   = "50100"
	defaultOrchClusterPort = "50104"
	defaultOrchSocket      = "/tmp/undf-orch.sock"
	defaultAgentSocket     = "/tmp/undf-agent.sock"
)

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// OrchestratorLocalPort is the local gRPC listener's port.
func OrchestratorLocalPort() string { return envOr(EnvOrchLocalPort, defaultOrchLocalPort) }

// OrchestratorClusterPort is the cluster listener's port.
func OrchestratorClusterPort() string { return envOr(EnvOrchClusterPort, defaultOrchClusterPort) }

// ClusterBind is the interface the cluster listener binds.
func ClusterBind() string { return envOr(EnvClusterBind, "0.0.0.0") }

// OrchestratorLocalAddr is the address a same-host agent dials.
func OrchestratorLocalAddr() string {
	return envOr(EnvOrchestratorAddr, "127.0.0.1:"+OrchestratorLocalPort())
}

// OrchestratorSocketPath is the orcli unix socket.
func OrchestratorSocketPath() string { return envOr(EnvOrchSocket, defaultOrchSocket) }

// AgentSocketPath is the ufagent unix socket.
func AgentSocketPath() string { return envOr(EnvAgentSocket, defaultAgentSocket) }

// ClusterStateDir is where cluster membership state is kept for role ("ca" on
// the manager, "node" on a worker). By default it is a sibling of the role's
// database, so it lives wherever that role's state already lives; with
// UNDERLEAF_CLUSTER_DIR it is a per-role subdirectory of that path. Roles never
// share a directory, so a single-host dev setup does not collide.
func ClusterStateDir(cfg Config, role string) string {
	if v := strings.TrimSpace(os.Getenv(EnvStateDir)); v != "" {
		return filepath.Join(v, role)
	}
	return filepath.Join(filepath.Dir(cfg.DBPath), "cluster-"+role)
}
