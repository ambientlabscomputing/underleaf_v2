package conn_client

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// listenerOn opens a real local listener and reports whether anything ever
// connected to it, so tests can prove a refused stream never reached the port.
func listenerOn(t *testing.T) (port int, connected func() bool) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	hit := make(chan struct{}, 8)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
			hit <- struct{}{}
		}
	}()
	return l.Addr().(*net.TCPAddr).Port, func() bool {
		select {
		case <-hit:
			return true
		case <-time.After(150 * time.Millisecond):
			return false
		}
	}
}

func TestForwardRefusesControlPlanePorts(t *testing.T) {
	t.Setenv("UNDERLEAF_CONFIG", "")
	t.Setenv(utils.EnvExtraControlPorts, "")
	cc := &ConnClient{}
	for _, port := range []int{9090, 9091, 50100, 50101, 50103, 2375, 0, -5, 65536} {
		_, err := cc.NewGatewayStream(context.Background(), "s1", types.StreamType("gateway"), port)
		if !errors.Is(err, utils.ErrControlPlanePort) {
			t.Fatalf("port %d: want ErrControlPlanePort, got %v", port, err)
		}
	}
}

// The refusal must happen at the dial: a real listener on a configured
// control-plane port must never see a connection.
func TestForwardNeverDialsDeniedPort(t *testing.T) {
	port, connected := listenerOn(t)

	// 1. Denied via the operator list.
	t.Setenv("UNDERLEAF_CONFIG", "")
	t.Setenv(utils.EnvExtraControlPorts, strconv.Itoa(port))
	_, err := (&ConnClient{}).NewGatewayStream(context.Background(), "s1", "gateway", port)
	if !errors.Is(err, utils.ErrControlPlanePort) {
		t.Fatalf("extra-list: want ErrControlPlanePort, got %v", err)
	}
	if connected() {
		t.Fatal("extra-list: agent dialed a denied port")
	}

	// 2. Denied because it is the configured agent/orchestrator REST port.
	t.Setenv(utils.EnvExtraControlPorts, "")
	cfg := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(cfg, []byte("http:\n  port: "+strconv.Itoa(port)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNDERLEAF_CONFIG", cfg)
	_, err = (&ConnClient{}).NewGatewayStream(context.Background(), "s2", "gateway", port)
	if !errors.Is(err, utils.ErrControlPlanePort) {
		t.Fatalf("configured port: want ErrControlPlanePort, got %v", err)
	}
	if connected() {
		t.Fatal("configured port: agent dialed a denied port")
	}
}

// Negative control: an ordinary port is not refused by the denylist; it gets
// as far as the (absent) connection check.
func TestForwardAllowsOrdinaryPort(t *testing.T) {
	t.Setenv("UNDERLEAF_CONFIG", "")
	t.Setenv(utils.EnvExtraControlPorts, "")
	port, _ := listenerOn(t)
	_, err := (&ConnClient{}).NewGatewayStream(context.Background(), "s1", "gateway", port)
	if err == nil || errors.Is(err, utils.ErrControlPlanePort) {
		t.Fatalf("ordinary port must pass the denylist (and fail later for lack of a connection), got %v", err)
	}
}
