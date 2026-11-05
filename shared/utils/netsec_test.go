package utils

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

func TestCheckBind(t *testing.T) {
	cases := []struct {
		name      string
		host      string
		protected bool
		token     string
		allow     string
		wantErr   bool
	}{
		{"loopback v4", "127.0.0.1", false, "", "", false},
		{"loopback v6", "::1", false, "", "", false},
		{"localhost", "localhost", false, "", "", false},
		{"wildcard refused", "0.0.0.0", false, "", "", true},
		{"empty host refused", "", false, "", "", true},
		{"lan refused", "192.168.1.5", true, "", "", true},
		{"token does not cover unprotected listener", "0.0.0.0", false, "secret", "", true},
		{"token covers protected listener", "0.0.0.0", true, "secret", "", false},
		{"explicit opt-in", "0.0.0.0", false, "", "1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvClusterToken, tc.token)
			t.Setenv(EnvInsecureAllowRemote, tc.allow)
			if err := CheckBind(tc.host, tc.protected); (err != nil) != tc.wantErr {
				t.Fatalf("CheckBind(%q) err=%v, wantErr=%v", tc.host, err, tc.wantErr)
			}
		})
	}
}

func TestListenTCPDefaultsToLoopback(t *testing.T) {
	t.Setenv(EnvBindAddr, "")
	lis, err := ListenTCP("0", false)
	if err != nil {
		t.Fatal(err)
	}
	defer lis.Close()
	if ip := lis.Addr().(*net.TCPAddr).IP; !ip.IsLoopback() {
		t.Fatalf("bound to %v, want loopback", ip)
	}
}

func TestListenTCPRefusesUnprotectedWildcard(t *testing.T) {
	t.Setenv(EnvBindAddr, "0.0.0.0")
	t.Setenv(EnvClusterToken, "")
	t.Setenv(EnvInsecureAllowRemote, "")
	if lis, err := ListenTCP("0", true); err == nil {
		lis.Close()
		t.Fatal("expected refusal")
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

func startHealthServer(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer(ClusterAuthServerOptions()...)
	healthpb.RegisterHealthServer(srv, health.NewServer())
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

func dial(t *testing.T, addr string, extra ...grpc.DialOption) healthpb.HealthClient {
	t.Helper()
	opts := append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, extra...)
	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return healthpb.NewHealthClient(conn)
}

func TestClusterTokenEnforced(t *testing.T) {
	t.Setenv(EnvClusterToken, "s3cret")
	addr := startHealthServer(t)
	ctx := context.Background()

	// No token.
	_, err := dial(t, addr).Check(ctx, &healthpb.HealthCheckRequest{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("no token: got %v, want Unauthenticated", err)
	}

	// Wrong token.
	t.Setenv(EnvClusterToken, "wrong")
	_, err = dial(t, addr, ClusterAuthDialOptions()...).Check(ctx, &healthpb.HealthCheckRequest{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("wrong token: got %v, want Unauthenticated", err)
	}

	// Streaming RPCs are gated too.
	stream, err := dial(t, addr).Watch(ctx, &healthpb.HealthCheckRequest{})
	if err == nil {
		_, err = stream.Recv()
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("stream without token: got %v, want Unauthenticated", err)
	}

	// Correct token.
	t.Setenv(EnvClusterToken, "s3cret")
	if _, err := dial(t, addr, ClusterAuthDialOptions()...).Check(ctx, &healthpb.HealthCheckRequest{}); err != nil {
		t.Fatalf("correct token rejected: %v", err)
	}
}

func TestNoTokenConfiguredLeavesServerOpen(t *testing.T) {
	t.Setenv(EnvClusterToken, "")
	if ClusterAuthServerOptions() != nil || ClusterAuthDialOptions() != nil {
		t.Fatal("expected no auth options when no token is configured")
	}
	addr := startHealthServer(t)
	if _, err := dial(t, addr).Check(context.Background(), &healthpb.HealthCheckRequest{}); err != nil {
		t.Fatal(err)
	}
}
