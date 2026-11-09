package utils

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckLocalBind(t *testing.T) {
	cases := []struct {
		name    string
		host    string
		wantErr bool
	}{
		{"loopback v4", "127.0.0.1", false},
		{"loopback v6", "::1", false},
		{"bracketed loopback v6", "[::1]", false},
		{"localhost", "localhost", false},
		{"wildcard refused", "0.0.0.0", true},
		{"v6 wildcard refused", "::", true},
		{"empty host refused", "", true},
		{"lan refused", "192.168.1.5", true},
		{"hostname refused", "manager.example.com", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckLocalBind(tc.host); (err != nil) != tc.wantErr {
				t.Fatalf("CheckLocalBind(%q) err=%v, wantErr=%v", tc.host, err, tc.wantErr)
			}
		})
	}
}

// Nothing in the environment widens a local bind: not the retired stopgap
// switches, and not the container-mode variable the Docker image used to set.
func TestNoEnvironmentVariableWidensALocalBind(t *testing.T) {
	t.Setenv("UNDERLEAF_CONTAINER", "1")
	t.Setenv("UNDERLEAF_INSECURE_ALLOW_REMOTE", "1")
	t.Setenv("UNDERLEAF_CLUSTER_TOKEN", "s3cret")
	for _, host := range []string{"0.0.0.0", "", "10.0.0.5"} {
		if err := CheckLocalBind(host); err == nil {
			t.Errorf("CheckLocalBind(%q) accepted a non-loopback bind because of an env var", host)
		}
	}
	t.Setenv(EnvBindAddr, "0.0.0.0")
	if lis, err := ListenLocal("0"); err == nil {
		lis.Close()
		t.Fatal("ListenLocal bound a wildcard address because of an env var")
	}
}

func TestListenLocalDefaultsToLoopback(t *testing.T) {
	t.Setenv(EnvBindAddr, "")
	lis, err := ListenLocal("0")
	if err != nil {
		t.Fatal(err)
	}
	defer lis.Close()
	if ip := lis.Addr().(*net.TCPAddr).IP; !ip.IsLoopback() {
		t.Fatalf("bound to %v, want loopback", ip)
	}
}

// unix socket paths are length-limited (104 bytes on macOS), so avoid t.TempDir().
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ulsec")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestListenUnixPrivate(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "s.sock")

	lis, err := ListenUnixPrivate(path)
	if err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket perm = %o, want 600", perm)
	}
	if err := VerifyOwnSocket(path); err != nil {
		t.Fatalf("VerifyOwnSocket: %v", err)
	}
	// Leave the socket file behind (as a crashed server would); a restart must replace it.
	lis.(*net.UnixListener).SetUnlinkOnClose(false)
	lis.Close()
	lis2, err := ListenUnixPrivate(path)
	if err != nil {
		t.Fatalf("replace stale socket: %v", err)
	}
	lis2.Close()
}

func TestListenUnixPrivateRefusesNonSocket(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "precious")
	if err := os.WriteFile(path, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if lis, err := ListenUnixPrivate(path); err == nil {
		lis.Close()
		t.Fatal("expected refusal to replace a regular file")
	}
	if b, _ := os.ReadFile(path); string(b) != "keep me" {
		t.Fatal("regular file was clobbered")
	}
	if err := VerifyOwnSocket(path); err == nil {
		t.Fatal("VerifyOwnSocket accepted a regular file")
	}
}
