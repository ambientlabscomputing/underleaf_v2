package utils

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestClusterStateDir(t *testing.T) {
	cfg := Config{DBPath: "/var/lib/underleaf/orchestrator.db"}

	t.Setenv(EnvStateDir, "")
	if got := ClusterStateDir(cfg, "ca"); got != "/var/lib/underleaf/cluster-ca" {
		t.Errorf("manager CA dir = %q, want beside the database", got)
	}
	node := ClusterStateDir(Config{DBPath: "agent.db"}, "node")
	if !filepath.IsAbs(node) || !strings.HasSuffix(node, filepath.Join("underleaf", "cluster-node")) {
		t.Errorf("worker credentials dir = %q, want an absolute per-user path independent of the working directory", node)
	}

	t.Setenv(EnvStateDir, "/scratch/state")
	ca, nd := ClusterStateDir(cfg, "ca"), ClusterStateDir(cfg, "node")
	if ca != "/scratch/state/ca" || nd != "/scratch/state/node" || ca == nd {
		t.Errorf("override: ca=%q node=%q; roles must get separate subdirectories", ca, nd)
	}
}

func TestEndpointOverrides(t *testing.T) {
	t.Setenv(EnvOrchLocalPort, "")
	t.Setenv(EnvOrchestratorAddr, "")
	t.Setenv(EnvOrchSocket, "")
	if OrchestratorLocalPort() != "50100" || OrchestratorLocalAddr() != "127.0.0.1:50100" || OrchestratorSocketPath() != "/tmp/undf-orch.sock" {
		t.Error("defaults changed")
	}
	t.Setenv(EnvOrchLocalPort, "25900")
	if OrchestratorLocalAddr() != "127.0.0.1:25900" {
		t.Errorf("local addr = %q", OrchestratorLocalAddr())
	}
	t.Setenv(EnvOrchestratorAddr, "127.0.0.1:1")
	if OrchestratorLocalAddr() != "127.0.0.1:1" {
		t.Error("explicit address ignored")
	}
}
