package utils

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckStreamPortDeniesControlPlaneDefaults(t *testing.T) {
	t.Setenv("UNDERLEAF_CONFIG", "")
	t.Setenv(EnvExtraControlPorts, "")
	for _, p := range []int{9090, 9091, 50100, 50101, 50103, 2375, 2376} {
		err := CheckStreamPort(p)
		if err == nil {
			t.Fatalf("port %d must be denied", p)
		}
		if !errors.Is(err, ErrControlPlanePort) {
			t.Fatalf("port %d: error must wrap ErrControlPlanePort, got %v", p, err)
		}
	}
}

func TestCheckStreamPortAllowsOrdinaryPorts(t *testing.T) {
	t.Setenv("UNDERLEAF_CONFIG", "")
	t.Setenv(EnvExtraControlPorts, "")
	for _, p := range []int{22, 80, 443, 3000, 5432, 8080, 5678, 65535} {
		if err := CheckStreamPort(p); err != nil {
			t.Fatalf("port %d should be allowed: %v", p, err)
		}
	}
}

func TestCheckStreamPortRejectsInvalidPorts(t *testing.T) {
	for _, p := range []int{0, -1, -9090, 65536, 1 << 20} {
		if err := CheckStreamPort(p); !errors.Is(err, ErrControlPlanePort) {
			t.Fatalf("port %d must be rejected, got %v", p, err)
		}
	}
}

func TestCheckStreamPortHonoursConfiguredPorts(t *testing.T) {
	// A non-default REST port from the config file must be denied, and the
	// default stays denied too.
	cfg := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(cfg, []byte("http:\n  port: 12345\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNDERLEAF_CONFIG", cfg)
	t.Setenv(EnvExtraControlPorts, "")
	if err := CheckStreamPort(12345); !errors.Is(err, ErrControlPlanePort) {
		t.Fatalf("configured REST port 12345 must be denied, got %v", err)
	}
	if err := CheckStreamPort(9090); !errors.Is(err, ErrControlPlanePort) {
		t.Fatalf("default port stays denied, got %v", err)
	}
	if err := CheckStreamPort(12346); err != nil {
		t.Fatalf("neighbouring port must stay allowed: %v", err)
	}
}

func TestCheckStreamPortHonoursExtraPortsEnv(t *testing.T) {
	t.Setenv("UNDERLEAF_CONFIG", "")
	t.Setenv(EnvExtraControlPorts, " 15090, 15100 ,notaport,0,99999,")
	for _, p := range []int{15090, 15100} {
		if err := CheckStreamPort(p); !errors.Is(err, ErrControlPlanePort) {
			t.Fatalf("extra port %d must be denied, got %v", p, err)
		}
	}
	for _, p := range []int{15091, 99999} {
		if p == 99999 {
			continue // invalid entries are ignored, and 99999 is invalid anyway
		}
		if err := CheckStreamPort(p); err != nil {
			t.Fatalf("port %d should be allowed: %v", p, err)
		}
	}
}
