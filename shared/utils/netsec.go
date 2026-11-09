package utils

import (
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
)

// Bind policy shared by the orchestrator and agent (RFD 5, "Two listeners").
//
// There are exactly two kinds of TCP listener in Underleaf:
//
//   - The *local* listeners (the Orchestrator's REST API and its local gRPC
//     listener) have no authentication by design: the trust boundary is the
//     local host. They therefore bind loopback, and ListenLocal refuses
//     anything else.
//   - The *cluster* listener is the only one allowed to face a network. It
//     cannot be built without mutual TLS (ListenCluster), so there is no
//     "insecure remote" switch and nothing to opt into.
//
// Agents have no TCP listeners at all; they dial out.
const (
	// EnvBindAddr is the interface the local listeners bind. Default 127.0.0.1.
	// Only loopback values are accepted: the Orchestrator runs as a native process
	// on the host and there is no deployment (container or otherwise) in which an
	// unauthenticated local listener is allowed to face a network.
	EnvBindAddr = "UNDERLEAF_BIND_ADDR"

	defaultBindAddr = "127.0.0.1"
)

// BindAddr returns the interface the local TCP listeners should bind to.
func BindAddr() string {
	if v := strings.TrimSpace(os.Getenv(EnvBindAddr)); v != "" {
		return v
	}
	return defaultBindAddr
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// CheckLocalBind returns an error when host would expose an unauthenticated
// local listener beyond the local host. Only loopback is accepted; the empty and
// wildcard hosts are refused.
func CheckLocalBind(host string) error {
	if isLoopbackHost(host) {
		return nil
	}
	return fmt.Errorf(
		"refusing to bind the unauthenticated local listener to non-loopback address %q: "+
			"local listeners have no authentication and are loopback-only. Worker nodes "+
			"connect through the cluster listener, which requires mTLS: run `orcli nodes add`",
		host)
}

// ListenLocal listens on BindAddr():port after applying CheckLocalBind.
func ListenLocal(port string) (net.Listener, error) {
	host := BindAddr()
	if err := CheckLocalBind(host); err != nil {
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
