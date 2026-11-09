package cluster

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentservice "github.com/ambientlabscomputing/underleaf_v2/edge/agent/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/pki"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// These drive the same JoinCluster function `ufagentd join` runs, against a
// real manager listener.

func TestAgentJoinClusterEndToEnd(t *testing.T) {
	e := newEnv(t)
	res, _ := e.be.cs.AddNode(e.addr)
	dir := filepath.Join(t.TempDir(), "node")
	var saved *types.Node
	out, err := agentservice.JoinCluster(context.Background(), res.JoinString, agentservice.JoinOptions{
		StateDir: dir,
		Host: func() (*agentservice.HostInfo, error) {
			return &agentservice.HostInfo{Hostname: "w1", IPAddr: "10.9.9.9", OS: "linux", Arch: "amd64"}, nil
		},
		SaveNode: func(n *types.Node) error { saved = n; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ReEnrolled || !pki.ValidNodeID(out.NodeID) {
		t.Fatalf("outcome = %+v", out)
	}
	if saved == nil || saved.ID != out.NodeID || saved.Name != "w1" {
		t.Errorf("agent-side node record = %+v", saved)
	}
	if err := agentservice.VerifyMembership(context.Background(), out.Member); err != nil {
		t.Fatalf("membership check: %v", err)
	}
	// The manager recorded the host details the node reported.
	n, err := e.be.repo.Nodes.GetNodeByID(out.NodeID)
	if err != nil || n.Name != "w1" || n.IPAddr != "10.9.9.9" || n.Transport != types.TransportCluster {
		t.Fatalf("manager's node row = %+v err=%v", n, err)
	}

	// Re-enrolling with a fresh string keeps the node id.
	res2, _ := e.be.cs.AddNode(e.addr)
	out2, err := agentservice.JoinCluster(context.Background(), res2.JoinString, agentservice.JoinOptions{StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if out2.NodeID != out.NodeID || !out2.ReEnrolled {
		t.Fatalf("re-enroll changed identity: %+v vs %+v", out2, out)
	}
}

func TestAgentJoinRefusesExpiredStringWithoutContactingTheManager(t *testing.T) {
	e := newEnv(t)
	res, _ := e.be.cs.AddNode(e.addr)
	before := e.srv.JoinAttempts()
	_, err := agentservice.JoinCluster(context.Background(), res.JoinString, agentservice.JoinOptions{
		StateDir: filepath.Join(t.TempDir(), "node"),
		Now:      func() time.Time { return time.Now().Add(time.Hour) },
	})
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err = %v, want an expiry error", err)
	}
	if e.srv.JoinAttempts() != before {
		t.Error("an expired string still reached the manager")
	}
}

func TestAgentJoinRejectsGarbageAndReusedStrings(t *testing.T) {
	e := newEnv(t)
	if _, err := agentservice.JoinCluster(context.Background(), "ulj_garbage", agentservice.JoinOptions{StateDir: filepath.Join(t.TempDir(), "n")}); err == nil {
		t.Error("garbage join string accepted")
	}
	res, _ := e.be.cs.AddNode(e.addr)
	if _, err := agentservice.JoinCluster(context.Background(), res.JoinString, agentservice.JoinOptions{StateDir: filepath.Join(t.TempDir(), "a")}); err != nil {
		t.Fatal(err)
	}
	dirB := filepath.Join(t.TempDir(), "b")
	if _, err := agentservice.JoinCluster(context.Background(), res.JoinString, agentservice.JoinOptions{StateDir: dirB}); err != pki.ErrJoinRejected {
		t.Fatalf("second machine with the same string: err = %v, want ErrJoinRejected", err)
	}
	if _, err := pki.LoadMember(dirB); err != pki.ErrNotMember {
		t.Error("a failed join left membership state behind")
	}
}

func TestAgentJoinWillNotSilentlySwitchClusters(t *testing.T) {
	e1, e2 := newEnv(t), newEnv(t)
	res1, _ := e1.be.cs.AddNode(e1.addr)
	dir := filepath.Join(t.TempDir(), "node")
	if _, err := agentservice.JoinCluster(context.Background(), res1.JoinString, agentservice.JoinOptions{StateDir: dir}); err != nil {
		t.Fatal(err)
	}
	res2, _ := e2.be.cs.AddNode(e2.addr)
	if _, err := agentservice.JoinCluster(context.Background(), res2.JoinString, agentservice.JoinOptions{StateDir: dir}); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v, want a refusal that mentions --force", err)
	}
	// The refused attempt must not have consumed the other cluster's secret.
	if _, err := agentservice.JoinCluster(context.Background(), res2.JoinString, agentservice.JoinOptions{StateDir: dir, Force: true}); err != nil {
		t.Fatalf("with --force: %v", err)
	}
}

func TestAgentJoinRefusesDamagedState(t *testing.T) {
	e := newEnv(t)
	res, _ := e.be.cs.AddNode(e.addr)
	dir := filepath.Join(t.TempDir(), "node")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "cluster.json"), []byte("{not json"), 0o600)
	if _, err := agentservice.JoinCluster(context.Background(), res.JoinString, agentservice.JoinOptions{StateDir: dir}); err == nil {
		t.Fatal("join proceeded over damaged state")
	}
}
