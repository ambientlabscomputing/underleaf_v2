package rest

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
)

// Authorization on the REST listener (RFD 5, step 6).
//
// How the paths coexist. Requests first pass requestGuard (Host/Origin/
// Content-Type), then authentication picks a principal:
//
//  1. "Authorization: Bearer <token>": a scoped API token. It is enforced
//     per route against routeScopes. A token that is malformed, unknown,
//     expired or revoked is a 401, never a fall-through to the local path.
//  2. WebSocket routes only: "?ticket=<ticket>", a single-use, short-lived
//     ticket (browsers cannot set headers on a WebSocket). It carries the
//     scope of whoever requested it. An invalid, used or expired ticket is
//     a 401.
//  3. Neither: the local principal. Per RFD "Local trust" the loopback
//     listener has no login, so the local UI/operator is admin-equivalent.
//     Setting UNDERLEAF_REQUIRE_TOKEN=1 turns this off: every API route
//     except /health then needs a token. Use it when the listener is
//     reachable by anything you do not trust as much as the machine's own
//     operator (see the README's note on published Docker ports).
//
// So on a default install a token narrows nothing for a caller who simply
// omits the header; tokens give automation a credential that is revocable,
// expiring and scoped, and are what strict mode and the remote listener
// (step 7b) enforce.

const envRequireToken = "UNDERLEAF_REQUIRE_TOKEN"

type routeKind int

const (
	routePublic routeKind = iota // no credential needed
	routeScoped                  // needs the given scope
)

type routeAccess struct {
	kind  routeKind
	scope service.Scope
	ws    bool // accepts a WebSocket ticket
}

// routeScopes is the whole route -> scope mapping, keyed by "METHOD /path"
// relative to the API base path. A route missing from this table requires
// admin (fail closed); a test asserts that every registered route is listed.
//
//	read   : read-only calls (GET-style), plus minting a log ticket
//	deploy : starting or changing workloads
//	admin  : everything else, including any future route not listed here
var routeScopes = map[string]routeAccess{
	"GET /health": {kind: routePublic},

	"GET /nodes":                            {kind: routeScoped, scope: service.ScopeRead},
	"GET /containers":                       {kind: routeScoped, scope: service.ScopeRead},
	"GET /nodes/:nodeId/containers":         {kind: routeScoped, scope: service.ScopeRead},
	"GET /containers/:dockerId/logs":        {kind: routeScoped, scope: service.ScopeRead},
	"GET /deployments":                      {kind: routeScoped, scope: service.ScopeRead},
	"GET /deployments/:id":                  {kind: routeScoped, scope: service.ScopeRead},
	"GET /containers/:dockerId/logs/stream": {kind: routeScoped, scope: service.ScopeRead, ws: true},
	"POST /ws-tickets":                      {kind: routeScoped, scope: service.ScopeRead},

	"POST /deployments": {kind: routeScoped, scope: service.ScopeDeploy},
}

// accessFor returns what a matched route requires, looking it up in
// routeScopes. The local and the remote listener share this one table; a route
// that is not in it requires admin (fail closed).
func accessFor(method, fullPath, base string) routeAccess {
	rel := strings.TrimPrefix(fullPath, base)
	if a, known := routeScopes[method+" "+rel]; known {
		return a
	}
	return routeAccess{kind: routeScoped, scope: service.ScopeAdmin}
}

// tokenVerifier authenticates bearer tokens (the token service in production).
type tokenVerifier interface {
	Authenticate(ctx context.Context, presented string) (*service.TokenPrincipal, error)
}

func (s *OrchestratorRESTServer) tokens() tokenVerifier {
	if s.tokenSrc != nil {
		return s.tokenSrc
	}
	return s.Service.Tokens()
}

// principal is who a request is acting as.
type principal struct {
	scope   service.Scope
	tokenID string // empty for the local principal
	local   bool
}

const principalKey = "underleaf.principal"

func principalFrom(c *gin.Context) (principal, bool) {
	v, ok := c.Get(principalKey)
	if !ok {
		return principal{}, false
	}
	p, ok := v.(principal)
	return p, ok
}

func requireTokenMode() bool { return os.Getenv(envRequireToken) == "1" }

func abortUnauthorized(c *gin.Context) {
	c.Header("WWW-Authenticate", `Bearer realm="underleaf"`)
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required or credential invalid"})
}

// authMiddleware authenticates and authorizes API requests. Requests that
// match no route (the UI's static files, 404s) pass through: the UI assets are
// not secret and every API path is a registered route.
func (s *OrchestratorRESTServer) authMiddleware(basePath string) gin.HandlerFunc {
	base := strings.TrimSuffix("/"+strings.Trim(basePath, "/"), "/")
	return func(c *gin.Context) {
		full := c.FullPath()
		if full == "" { // no route matched
			c.Next()
			return
		}
		access := accessFor(c.Request.Method, full, base)
		if access.kind == routePublic {
			c.Next()
			return
		}

		p, ok := s.authenticate(c, access)
		if !ok {
			return // response already written
		}
		if !p.scope.Allows(access.scope) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":    "insufficient scope",
				"required": string(access.scope),
			})
			return
		}
		c.Set(principalKey, p)
		c.Next()
	}
}

// authenticate resolves the request's principal, or writes a 401 and returns false.
func (s *OrchestratorRESTServer) authenticate(c *gin.Context, access routeAccess) (principal, bool) {
	// WebSocket ticket (browser path).
	if access.ws {
		if t := c.Query("ticket"); t != "" {
			if tp, ok := s.tickets().Redeem(t); ok {
				return principal{scope: tp.scope, tokenID: tp.tokenID, local: tp.local}, true
			}
			abortUnauthorized(c)
			return principal{}, false
		}
	}

	if h := c.GetHeader("Authorization"); h != "" {
		tok, ok := bearerToken(h)
		if !ok {
			abortUnauthorized(c)
			return principal{}, false
		}
		tp, err := s.tokens().Authenticate(c.Request.Context(), tok)
		if err != nil {
			if !errors.Is(err, service.ErrInvalidToken) {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "token check failed"})
				return principal{}, false
			}
			abortUnauthorized(c)
			return principal{}, false
		}
		return principal{scope: tp.Scope, tokenID: tp.TokenID}, true
	}

	if requireTokenMode() {
		abortUnauthorized(c)
		return principal{}, false
	}
	return principal{scope: service.ScopeAdmin, local: true}, true
}

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// value. The scheme is case-insensitive (RFC 7235); anything else, or an empty
// token, is not a bearer credential.
func bearerToken(header string) (string, bool) {
	const scheme = "Bearer "
	if len(header) <= len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return "", false
	}
	return strings.TrimSpace(header[len(scheme):]), true
}

// --- WebSocket tickets ------------------------------------------------------

const (
	wsTicketTTL = 30 * time.Second
	wsTicketMax = 1024
)

type ticketGrant struct {
	scope   service.Scope
	tokenID string
	local   bool
	expires time.Time
}

// ticketStore holds single-use, short-lived WebSocket tickets in memory. A
// ticket is a random 256-bit value; redeeming it removes it, so replaying a
// URL (from a log, history, or a proxy) does nothing.
type ticketStore struct {
	mu     sync.Mutex
	grants map[string]ticketGrant
	now    func() time.Time
}

func newTicketStore() *ticketStore {
	return &ticketStore{grants: map[string]ticketGrant{}, now: time.Now}
}

var errTooManyTickets = errors.New("too many outstanding tickets")

func (ts *ticketStore) Issue(p principal) (string, time.Duration, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", 0, err
	}
	id := "ulwt_" + base64.RawURLEncoding.EncodeToString(b)

	ts.mu.Lock()
	defer ts.mu.Unlock()
	now := ts.now()
	for k, g := range ts.grants { // drop expired entries
		if !now.Before(g.expires) {
			delete(ts.grants, k)
		}
	}
	if len(ts.grants) >= wsTicketMax {
		return "", 0, errTooManyTickets
	}
	ts.grants[id] = ticketGrant{scope: p.scope, tokenID: p.tokenID, local: p.local, expires: now.Add(wsTicketTTL)}
	return id, wsTicketTTL, nil
}

// Redeem consumes a ticket. It returns false for unknown, already used and
// expired tickets; the ticket is removed in every case it existed.
func (ts *ticketStore) Redeem(id string) (ticketGrant, bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	g, ok := ts.grants[id]
	if !ok {
		return ticketGrant{}, false
	}
	delete(ts.grants, id)
	if !ts.now().Before(g.expires) {
		return ticketGrant{}, false
	}
	return g, true
}

func (s *OrchestratorRESTServer) tickets() *ticketStore {
	s.ticketOnce.Do(func() {
		if s.ticketSrc == nil {
			s.ticketSrc = newTicketStore()
		}
	})
	return s.ticketSrc
}

func (s *OrchestratorRESTServer) RegisterAuthRoutes(router *gin.RouterGroup) {
	router.POST("/ws-tickets", s.CreateWSTicket)
}

// CreateWSTicket handles POST /ws-tickets: it mints a single-use ticket for
// opening a WebSocket as the caller (same scope, 30 s).
func (s *OrchestratorRESTServer) CreateWSTicket(c *gin.Context) {
	p, ok := principalFrom(c)
	if !ok {
		abortUnauthorized(c)
		return
	}
	ticket, ttl, err := s.tickets().Issue(p)
	if err != nil {
		if errors.Is(err, errTooManyTickets) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "too many outstanding tickets"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create ticket"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ticket": ticket, "expires_in": int(ttl.Seconds())})
}

// redactingLogFormatter is gin's access-log line (without colours) with
// WebSocket tickets scrubbed from the query string, so credentials never
// reach logs.
func redactingLogFormatter(p gin.LogFormatterParams) string {
	return fmt.Sprintf("[GIN] %s | %3d | %13v | %15s | %-7s %q\n%s",
		p.TimeStamp.Format("2006/01/02 - 15:04:05"), p.StatusCode, p.Latency, p.ClientIP,
		p.Method, redactTicket(p.Path), p.ErrorMessage)
}

func redactTicket(path string) string {
	i := strings.Index(path, "ticket=")
	if i < 0 {
		return path
	}
	j := strings.IndexByte(path[i:], '&')
	if j < 0 {
		return path[:i] + "ticket=REDACTED"
	}
	return path[:i] + "ticket=REDACTED" + path[i+j:]
}
