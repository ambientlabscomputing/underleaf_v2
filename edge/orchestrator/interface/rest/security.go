package rest

import (
	"mime"
	"net"
	"net/http"
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

// allowedOrigins is the browser-origin allowlist used by CORS, the Origin
// guard, and the log WebSocket.
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
		if origin := c.GetHeader("Origin"); origin != "" && !originAllowed(origin) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "origin not allowed"})
			return
		}
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			mt, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
			if mt != "application/json" {
				c.AbortWithStatusJSON(http.StatusUnsupportedMediaType, gin.H{"error": "Content-Type must be application/json"})
				return
			}
		}
		c.Next()
	}
}
