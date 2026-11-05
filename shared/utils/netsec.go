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
	// A non-loopback value is accepted only in container mode (below).
	EnvBindAddr = "UNDERLEAF_BIND_ADDR"
	// EnvContainerMode is set to "1" by the Underleaf Docker image. It is the one
	// recorded exception to "local listeners are loopback-only": inside a
	// container the local listeners must bind the container's interface for
	// Docker's port publishing to reach them, and the exposure is then decided by
	// how the host publishes the ports (install.sh publishes them on
	// 127.0.0.1 only). The setting is honoured only when the process really is
	// in a container (ContainerMode), so exporting it on a bare host does not
	// widen anything, and it never applies to the cluster listener, which is
	// governed by mTLS alone.
	EnvContainerMode = "UNDERLEAF_CONTAINER"

	defaultBindAddr = "127.0.0.1"
)

// containerMarkers are files that exist inside a container. A variable so tests
// can point at temporary files.
var containerMarkers = []string{"/.dockerenv", "/run/.containerenv"}

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

// ContainerMode reports whether this process was started as the Underleaf
// image's entrypoint: UNDERLEAF_CONTAINER=1 *and* a container marker file. Both
// are required, so neither a stray environment variable on a bare host nor an
// unrelated container is enough to widen a bind.
func ContainerMode() bool {
	if os.Getenv(EnvContainerMode) != "1" {
		return false
	}
	for _, m := range containerMarkers {
		if _, err := os.Stat(m); err == nil {
			return true
		}
	}
	return false
}

// CheckLocalBind returns an error when host would expose an unauthenticated
// local listener beyond the local host. Loopback is always fine; anything else
// (including the empty and wildcard hosts) only in container mode.
func CheckLocalBind(host string) error {
	if isLoopbackHost(host) {
		return nil
	}
	if ContainerMode() {
		return nil
	}
	return fmt.Errorf(
		"refusing to bind the unauthenticated local listener to non-loopback address %q: "+
			"local listeners have no authentication and are loopback-only (%s only widens this "+
			"inside the Underleaf Docker image). Worker nodes connect through the cluster "+
			"listener, which requires mTLS: run `orcli nodes add`",
		host, EnvBindAddr)
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
