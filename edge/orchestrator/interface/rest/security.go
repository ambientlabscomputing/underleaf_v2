package rest

import (
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// The REST API has no user authentication; it is meant to be driven by the
// local Orchestrator UI and CLI tools. These guards stop the two ways a web
// page the operator happens to visit could still drive it:
//
//   - CSRF: a cross-site form/fetch POST. CORS preflight alone doesn't stop
//     this, since a "simple" request (e.g. text/plain) skips it and gin binds
//     JSON regardless of Content-Type. We reject unknown Origins outright and
//     require application/json on bodies.
//   - DNS rebinding: an attacker's hostname resolving to 127.0.0.1 makes the
//     page same-origin with us, so Origin looks fine. We reject Host headers
//     that aren't a loopback name.

const (
	envAllowedOrigins = "UNDERLEAF_ALLOWED_ORIGINS" // comma-separated, added to defaults
	envAllowedHosts   = "UNDERLEAF_ALLOWED_HOSTS"   // comma-separated hostnames, added to defaults
)

var defaultAllowedOrigins = []string{"http://localhost:5173", "http://localhost:5183"}

var defaultAllowedHosts = []string{"localhost", "127.0.0.1", "::1"}

func splitEnv(key string) []string {
	var out []string
	for v := range strings.SplitSeq(os.Getenv(key), ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// allowedOrigins is the extra browser-origin allowlist used by the Origin
// guard and the log WebSocket. The UI's own origin never needs listing: see
// sameOrigin.
func allowedOrigins() []string {
	return append(append([]string{}, defaultAllowedOrigins...), splitEnv(envAllowedOrigins)...)
}

func originAllowed(origin string) bool {
	for _, o := range allowedOrigins() {
		if strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}

// sameOrigin reports whether origin is this server's own origin: its
// authority equals the request's Host, and that Host is itself an allowed
// (loopback) name. This is what lets the UI be served from the API's own
// origin on whatever port it is published, without any configuration and
// without trusting any foreign origin. A page on another host or port cannot
// pass it, and a rebound hostname fails the Host check.
func sameOrigin(origin, host string) bool {
	if !hostAllowed(host) {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

// originPermitted is the one Origin policy shared by the REST guard and the
// WebSocket upgrader.
func originPermitted(origin, host string) bool {
	return originAllowed(origin) || sameOrigin(origin, host)
}

func hostAllowed(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	for _, a := range append(append([]string{}, defaultAllowedHosts...), splitEnv(envAllowedHosts)...) {
		if strings.EqualFold(a, host) {
			return true
		}
	}
	return false
}

// requestGuard rejects requests that fail the Host, Origin, or Content-Type
// checks. Requests without an Origin header (curl, the CLI, server-side
// callers) are not browser-initiated and pass the Origin check.
func requestGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !hostAllowed(c.Request.Host) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "host not allowed"})
			return
		}
		if origin := c.GetHeader("Origin"); origin != "" && !originPermitted(origin, c.Request.Host) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "origin not allowed"})
			return
		}
		if !requireJSONBody(c) {
			return
		}
		c.Next()
	}
}

// requireJSONBody aborts requests whose body is not declared as JSON, and
// reports whether the request may continue.
func requireJSONBody(c *gin.Context) bool {
	switch c.Request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		mt, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if mt != "application/json" {
			c.AbortWithStatusJSON(http.StatusUnsupportedMediaType, gin.H{"error": "Content-Type must be application/json"})
			return false
		}
	}
	return true
}
