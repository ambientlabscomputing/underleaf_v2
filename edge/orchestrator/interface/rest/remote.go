package rest

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/remote"
	"github.com/ambientlabscomputing/underleaf_v2/shared/assertion"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// The remote listener (RFD 5, step 7b).
//
// Cockpit and `ufctl connect` reach a cluster through the cluster's outbound
// cloud tunnel. The agent hands those tunnelled requests to this listener,
// which is a second HTTP server, separate from the local one, serving the same
// UI and API handlers (one origin) behind a different, strict middleware chain:
//
//   - EVERY request needs a valid cloud-signed assertion in the
//     X-Underleaf-Assertion header (shared/assertion.HeaderName): signature
//     by a cached cloud key, audience = this cluster, unexpired, unreplayed jti.
//     Static UI files and /health included. Nothing is unauthenticated here.
//   - The assertion's scope is checked against the SAME route table as local API
//     tokens (routeScopes / accessFor). A route not in the table needs admin.
//   - Authorization headers, API tokens, WebSocket tickets and any
//     forwarded-identity headers are not credentials on this listener.
//   - The Host and Origin guards do not apply: the browser's Host/Origin are the
//     cloud's, and the cloud authenticated the request before signing for it
//     (protecting its own sessions against cross-site requests is the cloud's
//     job). The Content-Type rule still does.
//
// The local listener is the mirror image: it never reads an assertion and
// refuses any request that carries one (rejectRemoteCredentials), so tunnelled
// traffic that somehow lands there fails closed instead of inheriting the
// unauthenticated local principal.

// EnvRemoteAddr is host:port for the remote listener. Unset or empty disables
// it (the default): no remote access unless the operator turns it on. It shares
// the local listeners' bind policy (utils.CheckLocalBind): loopback, or the
// container interface inside the Docker image. Its only client is the agent's
// tunnel on the same host, so it has no reason to face a network; and unlike the
// cluster listener it is plain HTTP authenticated by a bearer-style assertion,
// so it must not be exposed beyond loopback.
const EnvRemoteAddr = utils.EnvRemoteAddr

// remoteAuthenticator verifies an assertion. *remote.Verifier in production.
type remoteAuthenticator interface {
	Authenticate(ctx context.Context, token string) (*remote.Principal, error)
}

const remoteKey = "underleaf.remote"

func isRemote(c *gin.Context) bool { return c.GetBool(remoteKey) }

// RemoteHandler returns the handler for the remote listener: the one-origin
// UI + API behind assertion authentication.
func (s *OrchestratorRESTServer) RemoteHandler(basePath, uiDir string) http.Handler {
	return s.buildRouter(basePath, uiDir, listenerRemote)
}

func (s *OrchestratorRESTServer) remoteAuthMiddleware(basePath string) gin.HandlerFunc {
	base := strings.TrimSuffix("/"+strings.Trim(basePath, "/"), "/")
	return func(c *gin.Context) {
		fail := func(status int, msg string, err error) {
			if err != nil {
				_ = c.Error(err) // reaches the access log, never the client
			}
			if status == http.StatusUnauthorized {
				c.Header("WWW-Authenticate", "Underleaf-Assertion")
			}
			c.AbortWithStatusJSON(status, gin.H{"error": msg})
		}
		if s.Remote == nil {
			fail(http.StatusServiceUnavailable, "remote access is not configured", nil)
			return
		}
		vals := c.Request.Header.Values(assertion.HeaderName)
		if len(vals) != 1 || strings.TrimSpace(vals[0]) == "" {
			fail(http.StatusUnauthorized, "assertion required", nil)
			return
		}
		p, err := s.Remote.Authenticate(c.Request.Context(), strings.TrimSpace(vals[0]))
		if err != nil {
			fail(http.StatusUnauthorized, "assertion invalid", err)
			return
		}
		if full := c.FullPath(); full != "" {
			if a := accessFor(c.Request.Method, full, base); a.kind == routeScoped && !p.Scope.Allows(a.scope) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error":    "insufficient scope",
					"required": string(a.scope),
				})
				return
			}
		}
		c.Set(principalKey, principal{scope: p.Scope, tokenID: "assertion:" + p.JTI})
		c.Set(remoteKey, true)
		c.Next()
	}
}

// remoteGuard is the remote listener's request guard: bodies must be JSON.
// See the file comment for why Host and Origin are not checked here.
func remoteGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireJSONBody(c) {
			return
		}
		c.Next()
	}
}

// rejectRemoteCredentials makes the local listener refuse requests carrying
// remote-access credentials or forwarded-identity headers. The local listener
// trusts the machine's own operator without a login, so anything that looks
// like it came from the cloud must not reach that path.
func rejectRemoteCredentials() gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, h := range []string{assertion.HeaderName, "X-Subject-Id"} {
			if len(c.Request.Header.Values(h)) > 0 {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "remote credentials are not accepted on the local listener"})
				return
			}
		}
		c.Next()
	}
}

// ServeRemote runs the remote listener if UNDERLEAF_REMOTE_ADDR is set. It
// returns nil immediately when remote access is not enabled, and blocks while
// serving.
func (s *OrchestratorRESTServer) ServeRemote() error {
	raw := strings.TrimSpace(os.Getenv(EnvRemoteAddr))
	if raw == "" {
		return nil
	}
	if s.Remote == nil {
		return fmt.Errorf("%s is set but no assertion verifier is configured", EnvRemoteAddr)
	}
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return fmt.Errorf("%s=%q: %w", EnvRemoteAddr, raw, err)
	}
	if err := utils.CheckLocalBind(host); err != nil {
		return fmt.Errorf("remote listener: %w", err)
	}

	config := utils.GetConfig(utils.OrchestratorConfig)
	uiDir := s.UIDir
	if uiDir == "" {
		uiDir = os.Getenv(envUIDir)
	}
	srv := &http.Server{
		Addr:         net.JoinHostPort(host, port),
		Handler:      s.RemoteHandler(config.Http.BasePath.String(), uiDir),
		ReadTimeout:  utils.ParseDuration(config.Http.ReadTimeout),
		WriteTimeout: utils.ParseDuration(config.Http.WriteTimeout),
		IdleTimeout:  utils.ParseDuration(config.Http.IdleTimeout),
	}
	fmt.Printf("Remote listener (assertion required) on http://%s\n", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
