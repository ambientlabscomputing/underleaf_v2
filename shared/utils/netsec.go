package utils

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Local-listener hardening shared by the orchestrator and agent.
//
// None of the edge listeners (REST, gRPC, unix sockets) carry per-user
// authentication, so the trust boundary is "who can reach the listener".
// These helpers keep that boundary at the local host by default and make
// widening it an explicit, loud decision.
const (
	// EnvBindAddr is the interface TCP listeners bind to. Default 127.0.0.1.
	EnvBindAddr = "UNDERLEAF_BIND_ADDR"
	// EnvClusterToken is a shared secret required on agent<->orchestrator gRPC.
	EnvClusterToken = "UNDERLEAF_CLUSTER_TOKEN"
	// EnvInsecureAllowRemote acknowledges a non-loopback bind with no
	// application-level protection (e.g. inside a container whose published
	// ports are restricted by the container runtime).
	EnvInsecureAllowRemote = "UNDERLEAF_INSECURE_ALLOW_REMOTE"

	defaultBindAddr = "127.0.0.1"
)

// BindAddr returns the interface TCP listeners should bind to.
func BindAddr() string {
	if v := strings.TrimSpace(os.Getenv(EnvBindAddr)); v != "" {
		return v
	}
	return defaultBindAddr
}

// ClusterToken returns the configured agent<->orchestrator shared secret, or "".
func ClusterToken() string {
	return os.Getenv(EnvClusterToken)
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// CheckBind returns an error when addr would expose a listener beyond the
// local host without protection. A non-loopback bind is accepted only if the
// listener enforces the cluster token (tokenProtected) or the operator set
// UNDERLEAF_INSECURE_ALLOW_REMOTE=1. Empty and wildcard hosts count as remote.
func CheckBind(host string, tokenProtected bool) error {
	if isLoopbackHost(host) {
		return nil
	}
	if tokenProtected && ClusterToken() != "" {
		return nil
	}
	if os.Getenv(EnvInsecureAllowRemote) == "1" {
		return nil
	}
	return fmt.Errorf(
		"refusing to bind to non-loopback address %q without protection: set %s, "+
			"or set %s=1 to accept the risk (e.g. when the container runtime restricts published ports)",
		host, EnvClusterToken, EnvInsecureAllowRemote)
}

// ListenTCP listens on BindAddr():port after applying CheckBind.
// tokenProtected says whether the caller enforces the cluster token.
func ListenTCP(port string, tokenProtected bool) (net.Listener, error) {
	host := BindAddr()
	if err := CheckBind(host, tokenProtected); err != nil {
		return nil, err
	}
	return net.Listen("tcp", net.JoinHostPort(host, port))
}

// ListenUnixPrivate listens on a unix socket usable only by the current user.
// A stale socket at path is replaced; anything else there (a regular file, a
// socket owned by someone else) is refused rather than deleted.
func ListenUnixPrivate(path string) (net.Listener, error) {
	if fi, err := os.Lstat(path); err == nil {
		if err := checkOwnSocket(fi, path); err != nil {
			return nil, fmt.Errorf("existing path is not a socket owned by this user: %w", err)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale socket: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	lis, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		lis.Close()
		return nil, fmt.Errorf("chmod socket: %w", err)
	}
	return lis, nil
}

// VerifyOwnSocket checks that path is a unix socket owned by the current user.
// CLIs call it before dialing a socket in a shared directory such as /tmp, so
// another local user can't squat the path and capture requests (which can
// carry GitHub tokens).
func VerifyOwnSocket(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	return checkOwnSocket(fi, path)
}

func checkOwnSocket(fi os.FileInfo, path string) error {
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s is not a unix socket", path)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s: cannot determine owner", path)
	}
	if int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s is owned by uid %d, not the current user (uid %d)", path, st.Uid, os.Getuid())
	}
	return nil
}

// --- cluster-token gRPC auth -------------------------------------------------

const authMetadataKey = "authorization"

func checkClusterToken(ctx context.Context, want string) error {
	md, _ := metadata.FromIncomingContext(ctx)
	for _, v := range md.Get(authMetadataKey) {
		got := strings.TrimPrefix(v, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1 {
			return nil
		}
	}
	return status.Error(codes.Unauthenticated, "missing or invalid cluster token")
}

// ClusterAuthServerOptions returns server options enforcing the cluster token
// on every RPC, or nil when no token is configured.
func ClusterAuthServerOptions() []grpc.ServerOption {
	want := ClusterToken()
	if want == "" {
		return nil
	}
	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
			if err := checkClusterToken(ctx, want); err != nil {
				return nil, err
			}
			return h(ctx, req)
		}),
		grpc.ChainStreamInterceptor(func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, h grpc.StreamHandler) error {
			if err := checkClusterToken(ss.Context(), want); err != nil {
				return err
			}
			return h(srv, ss)
		}),
	}
}

type tokenCreds struct{ token string }

func (t tokenCreds) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{authMetadataKey: "Bearer " + t.token}, nil
}

// RequireTransportSecurity is false: the edge gRPC links are plaintext today,
// so the token gates access but does not defend against a network sniffer.
func (tokenCreds) RequireTransportSecurity() bool { return false }

// ClusterAuthDialOptions returns dial options attaching the cluster token to
// every RPC, or nil when no token is configured.
func ClusterAuthDialOptions() []grpc.DialOption {
	token := ClusterToken()
	if token == "" {
		return nil
	}
	return []grpc.DialOption{grpc.WithPerRPCCredentials(tokenCreds{token})}
}
