package utils

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// fakeContainer makes ContainerMode's file marker exist (or not) for a test.
func fakeContainer(t *testing.T, inContainer bool) {
	t.Helper()
	old := containerMarkers
	t.Cleanup(func() { containerMarkers = old })
	if !inContainer {
		containerMarkers = []string{filepath.Join(t.TempDir(), "no-such-marker")}
		return
	}
	marker := filepath.Join(t.TempDir(), ".dockerenv")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	containerMarkers = []string{marker}
}

func TestCheckLocalBind(t *testing.T) {
	cases := []struct {
		name         string
		host         string
		inContainer  bool
		containerEnv string
		wantErr      bool
	}{
		{"loopback v4", "127.0.0.1", false, "", false},
		{"loopback v6", "::1", false, "", false},
		{"bracketed loopback v6", "[::1]", false, "", false},
		{"localhost", "localhost", false, "", false},
		{"wildcard refused", "0.0.0.0", false, "", true},
		{"v6 wildcard refused", "::", false, "", true},
		{"empty host refused", "", false, "", true},
		{"lan refused", "192.168.1.5", false, "", true},
		{"hostname refused", "manager.example.com", false, "", true},
		// Container mode needs BOTH the image's env marker and a real container.
		{"env marker on a bare host does not widen", "0.0.0.0", false, "1", true},
		{"a container without the marker does not widen", "0.0.0.0", true, "", true},
		{"marker with a wrong value does not widen", "0.0.0.0", true, "true", true},
		{"the Docker image (marker + container)", "0.0.0.0", true, "1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeContainer(t, tc.inContainer)
			t.Setenv(EnvContainerMode, tc.containerEnv)
			if err := CheckLocalBind(tc.host); (err != nil) != tc.wantErr {
				t.Fatalf("CheckLocalBind(%q) err=%v, wantErr=%v", tc.host, err, tc.wantErr)
			}
		})
	}
}

// The stopgap switches are gone: setting them must not widen anything.
func TestRetiredStopgapsHaveNoEffect(t *testing.T) {
	fakeContainer(t, false)
	t.Setenv(EnvContainerMode, "")
	t.Setenv("UNDERLEAF_INSECURE_ALLOW_REMOTE", "1")
	t.Setenv("UNDERLEAF_CLUSTER_TOKEN", "s3cret")
	for _, host := range []string{"0.0.0.0", "", "10.0.0.5"} {
		if err := CheckLocalBind(host); err == nil {
			t.Errorf("CheckLocalBind(%q) accepted a non-loopback bind because of a retired env var", host)
		}
	}
	t.Setenv(EnvBindAddr, "0.0.0.0")
	if lis, err := ListenLocal("0"); err == nil {
		lis.Close()
		t.Fatal("ListenLocal bound a wildcard address because of a retired env var")
	}
}

func TestListenLocalDefaultsToLoopback(t *testing.T) {
	fakeContainer(t, false)
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

func TestListenLocalRefusesWildcardOutsideTheImage(t *testing.T) {
	fakeContainer(t, false)
	t.Setenv(EnvBindAddr, "0.0.0.0")
	t.Setenv(EnvContainerMode, "1") // set on a bare host: ignored
	if lis, err := ListenLocal("0"); err == nil {
		lis.Close()
		t.Fatal("expected refusal")
	}
}

func TestListenLocalInTheImageBindsTheContainerInterface(t *testing.T) {
	fakeContainer(t, true)
	t.Setenv(EnvBindAddr, "0.0.0.0")
	t.Setenv(EnvContainerMode, "1")
	lis, err := ListenLocal("0")
	if err != nil {
		t.Fatalf("the Docker image's local listener must still bind all interfaces: %v", err)
	}
	defer lis.Close()
	if ip := lis.Addr().(*net.TCPAddr).IP; !ip.IsUnspecified() {
		t.Fatalf("bound to %v, want the wildcard address", ip)
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
