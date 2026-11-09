package rest

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	_ "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository/migrations"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
)

// authEnv is a real token service (migrated sqlite) with a controllable clock.
type authEnv struct {
	svc *service.TokenService
	now *time.Time
}

func newAuthEnv(t *testing.T) *authEnv {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(cfg, []byte("db_path: "+filepath.Join(dir, "orch.db")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNDERLEAF_CONFIG", cfg)
	t.Setenv(envRequireToken, "")
	t.Setenv(envAllowedOrigins, "")
	t.Setenv(envAllowedHosts, "")
	repo, err := repository.NewRepository()
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Start(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	return &authEnv{
		svc: service.NewTokenService(repo.APITokens()).WithClock(func() time.Time { return now }),
		now: &now,
	}
}

func (e *authEnv) mint(t *testing.T, scope service.Scope, ttl time.Duration) string {
	t.Helper()
	c, err := e.svc.Create("t", scope, ttl)
	if err != nil {
		t.Fatal(err)
	}
	return c.Secret
}

func (e *authEnv) server(t *testing.T) (*httptest.Server, *OrchestratorRESTServer) {
	t.Helper()
	s := &OrchestratorRESTServer{logSrc: fakeLogs{}, tokenSrc: e.svc}
	srv := httptest.NewServer(s.newRouter(apiBase, ""))
	t.Cleanup(srv.Close)
	return srv, s
}

func bearer(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }

func withJSON(h map[string]string) map[string]string {
	out := map[string]string{"Content-Type": "application/json"}
	for k, v := range h {
		out[k] = v
	}
	return out
}

// --- scope enforcement, on the real router --------------------------------

func TestScopesEnforcedPerRoute(t *testing.T) {
	e := newAuthEnv(t)
	srv, _ := e.server(t)
	tok := map[service.Scope]string{
		service.ScopeRead:   e.mint(t, service.ScopeRead, time.Hour),
		service.ScopeDeploy: e.mint(t, service.ScopeDeploy, time.Hour),
		service.ScopeAdmin:  e.mint(t, service.ScopeAdmin, time.Hour),
	}
	// Two routes whose handlers need no database: a read route and a deploy route
	// (a valid-scope POST reaches the handler, which rejects the empty body with 400).
	type probe struct {
		name, method, path, body string
		reached                  int
		need                     service.Scope
	}
	probes := []probe{
		{"read route (logs)", "GET", "/api/v1/containers/abc/logs", "", 200, service.ScopeRead},
		{"deploy route", "POST", "/api/v1/deployments", `{}`, 400, service.ScopeDeploy},
		{"ticket route", "POST", "/api/v1/ws-tickets", `{}`, 200, service.ScopeRead},
	}
	for _, p := range probes {
		for scope, secret := range tok {
			resp, body := reqWith(t, p.method, srv.URL+p.path, p.body, withJSON(bearer(secret)))
			if scope.Allows(p.need) {
				if resp.StatusCode != p.reached {
					t.Fatalf("%s with %s token: want %d (authorized), got %d %s", p.name, scope, p.reached, resp.StatusCode, body)
				}
			} else if resp.StatusCode != 403 || !strings.Contains(body, string(p.need)) {
				t.Fatalf("%s with %s token: want 403 naming %s, got %d %s", p.name, scope, p.need, resp.StatusCode, body)
			}
		}
	}
	// The headline cases, spelled out.
	if resp, _ := reqWith(t, "POST", srv.URL+"/api/v1/deployments", `{}`, withJSON(bearer(tok[service.ScopeRead]))); resp.StatusCode != 403 {
		t.Fatalf("a read token must not deploy, got %d", resp.StatusCode)
	}
	if resp, _ := reqWith(t, "POST", srv.URL+"/api/v1/deployments", `{}`, withJSON(bearer(tok[service.ScopeDeploy]))); resp.StatusCode == 403 || resp.StatusCode == 401 {
		t.Fatalf("a deploy token must be allowed to deploy, got %d", resp.StatusCode)
	}
	if resp, _ := get(t, srv.URL+"/api/v1/containers/abc/logs", bearer(tok[service.ScopeRead])); resp.StatusCode != 200 {
		t.Fatalf("a read token must read, got %d", resp.StatusCode)
	}
}

func TestEveryRegisteredRouteHasAnExplicitScope(t *testing.T) {
	s := &OrchestratorRESTServer{logSrc: fakeLogs{}}
	router := s.newRouter(apiBase, "")
	if len(router.Routes()) == 0 {
		t.Fatal("no routes")
	}
	for _, r := range router.Routes() {
		rel := strings.TrimPrefix(r.Path, "/api/v1")
		if _, ok := routeScopes[r.Method+" "+rel]; !ok {
			t.Errorf("route %s %s has no entry in routeScopes (it would silently require admin)", r.Method, r.Path)
		}
	}
	for k, a := range routeScopes {
		if a.kind == routeScoped && a.scope.Allows(service.ScopeAdmin) && a.scope != service.ScopeAdmin {
			t.Errorf("%s: invalid scope %q", k, a.scope)
		}
		found := false
		for _, r := range router.Routes() {
			if r.Method+" "+strings.TrimPrefix(r.Path, "/api/v1") == k {
				found = true
			}
		}
		if !found {
			t.Errorf("routeScopes has a stale entry %q with no matching route", k)
		}
	}
	// Mutating or workload-starting routes must not be read-scoped.
	for k, a := range routeScopes {
		if strings.HasPrefix(k, "POST ") && a.kind == routeScoped && k != "POST /ws-tickets" && a.scope == service.ScopeRead {
			t.Errorf("%s is a POST but read-scoped", k)
		}
	}
}

func TestUnmappedRouteRequiresAdmin(t *testing.T) {
	e := newAuthEnv(t)
	gin.SetMode(gin.TestMode)
	s := &OrchestratorRESTServer{tokenSrc: e.svc}
	r := gin.New()
	r.Use(s.authMiddleware(apiBase))
	r.GET("/api/v1/brand-new-route", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	srv := httptest.NewServer(r)
	defer srv.Close()

	for scope, want := range map[service.Scope]int{service.ScopeRead: 403, service.ScopeDeploy: 403, service.ScopeAdmin: 200} {
		resp, _ := get(t, srv.URL+"/api/v1/brand-new-route", bearer(e.mint(t, scope, time.Hour)))
		if resp.StatusCode != want {
			t.Fatalf("%s on an unmapped route: got %d, want %d", scope, resp.StatusCode, want)
		}
	}
}

// --- bad credentials --------------------------------------------------------

func TestBadTokensAreRejectedNotDemotedToLocal(t *testing.T) {
	e := newAuthEnv(t)
	srv, _ := e.server(t)
	url := srv.URL + "/api/v1/containers/abc/logs"

	good := e.mint(t, service.ScopeAdmin, time.Hour)
	if resp, _ := get(t, url, bearer(good)); resp.StatusCode != 200 {
		t.Fatalf("sanity: good token %d", resp.StatusCode)
	}
	flipped := good[:len(good)-1] + map[bool]string{true: "1", false: "0"}[good[len(good)-1] == '0']

	revoked := e.mint(t, service.ScopeAdmin, time.Hour)
	id := strings.Split(strings.TrimPrefix(revoked, "ulat_"), "_")[0]
	if err := e.svc.Revoke(id); err != nil {
		t.Fatal(err)
	}
	expired := e.mint(t, service.ScopeAdmin, time.Minute)

	cases := map[string]map[string]string{
		"wrong secret":     bearer(flipped),
		"revoked token":    bearer(revoked),
		"garbage":          bearer("not-a-token"),
		"empty bearer":     {"Authorization": "Bearer "},
		"basic scheme":     {"Authorization": "Basic " + good},
		"token no scheme":  {"Authorization": good},
		"bearer, 2 tokens": {"Authorization": "Bearer " + good + " " + good},
	}
	for name, h := range cases {
		resp, _ := get(t, url, h)
		if resp.StatusCode != 401 || resp.Header.Get("WWW-Authenticate") == "" {
			t.Fatalf("%s: want 401 + WWW-Authenticate, got %d", name, resp.StatusCode)
		}
	}
	// Expiry: valid now, rejected once the clock passes it.
	if resp, _ := get(t, url, bearer(expired)); resp.StatusCode != 200 {
		t.Fatalf("not yet expired: %d", resp.StatusCode)
	}
	*e.now = e.now.Add(2 * time.Minute)
	if resp, _ := get(t, url, bearer(expired)); resp.StatusCode != 401 {
		t.Fatalf("expired token: want 401, got %d", resp.StatusCode)
	}
	// Case-insensitive scheme is accepted.
	if resp, _ := get(t, url, map[string]string{"Authorization": "bearer " + good}); resp.StatusCode != 200 {
		t.Fatalf("lowercase scheme: %d", resp.StatusCode)
	}
}

func TestRevokedTokenStopsWorkingImmediately(t *testing.T) {
	e := newAuthEnv(t)
	srv, _ := e.server(t)
	tok := e.mint(t, service.ScopeRead, time.Hour)
	url := srv.URL + "/api/v1/containers/abc/logs"
	if resp, _ := get(t, url, bearer(tok)); resp.StatusCode != 200 {
		t.Fatalf("before revoke: %d", resp.StatusCode)
	}
	if err := e.svc.Revoke(strings.Split(strings.TrimPrefix(tok, "ulat_"), "_")[0]); err != nil {
		t.Fatal(err)
	}
	if resp, _ := get(t, url, bearer(tok)); resp.StatusCode != 401 {
		t.Fatalf("after revoke: want 401, got %d", resp.StatusCode)
	}
}

// --- local path and strict mode --------------------------------------------

func TestLocalPathUnchangedByDefaultAndStrictModeClosesIt(t *testing.T) {
	e := newAuthEnv(t)
	srv, _ := e.server(t)
	logs := srv.URL + "/api/v1/containers/abc/logs"

	// Default: no header = the local operator/UI, full access (RFD "Local trust").
	if resp, _ := get(t, logs, nil); resp.StatusCode != 200 {
		t.Fatalf("default install must keep working with no token: %d", resp.StatusCode)
	}
	if resp, _ := reqWith(t, "POST", srv.URL+"/api/v1/deployments", `{}`, withJSON(nil)); resp.StatusCode != 400 {
		t.Fatalf("local deploy should reach the handler: %d", resp.StatusCode)
	}

	// Strict mode: API needs a token; /health stays open.
	t.Setenv(envRequireToken, "1")
	if resp, _ := get(t, logs, nil); resp.StatusCode != 401 {
		t.Fatalf("strict mode, no token: want 401, got %d", resp.StatusCode)
	}
	if resp, _ := reqWith(t, "POST", srv.URL+"/api/v1/deployments", `{}`, withJSON(nil)); resp.StatusCode != 401 {
		t.Fatalf("strict mode deploy, no token: want 401, got %d", resp.StatusCode)
	}
	if resp, _ := get(t, srv.URL+"/api/v1/health", nil); resp.StatusCode != 200 {
		t.Fatalf("health stays public: %d", resp.StatusCode)
	}
	if resp, _ := get(t, logs, bearer(e.mint(t, service.ScopeRead, time.Hour))); resp.StatusCode != 200 {
		t.Fatalf("strict mode with a token: %d", resp.StatusCode)
	}
}

func TestGuardsRunBeforeAuthentication(t *testing.T) {
	e := newAuthEnv(t)
	srv, _ := e.server(t)
	tok := e.mint(t, service.ScopeAdmin, time.Hour)
	url := srv.URL + "/api/v1/containers/abc/logs"

	h := bearer(tok)
	h["Host"] = "evil.example"
	if resp, _ := get(t, url, h); resp.StatusCode != 403 {
		t.Fatalf("a valid token must not bypass the Host guard: %d", resp.StatusCode)
	}
	h = bearer(tok)
	h["Origin"] = "https://evil.example"
	if resp, _ := get(t, url, h); resp.StatusCode != 403 {
		t.Fatalf("a valid token must not bypass the Origin guard: %d", resp.StatusCode)
	}
	h = bearer(tok)
	h["Content-Type"] = "text/plain"
	if resp, _ := reqWith(t, "POST", srv.URL+"/api/v1/deployments", `{}`, h); resp.StatusCode != 415 {
		t.Fatalf("a valid token must not bypass the Content-Type guard: %d", resp.StatusCode)
	}
}

// --- WebSocket tickets ------------------------------------------------------

func mintTicket(t *testing.T, srv *httptest.Server, h map[string]string) string {
	t.Helper()
	resp, body := reqWith(t, "POST", srv.URL+"/api/v1/ws-tickets", `{}`, withJSON(h))
	if resp.StatusCode != 200 {
		t.Fatalf("mint ticket: %d %s", resp.StatusCode, body)
	}
	i := strings.Index(body, `"ticket":"`)
	if i < 0 {
		t.Fatalf("no ticket in %s", body)
	}
	rest := body[i+len(`"ticket":"`):]
	return rest[:strings.IndexByte(rest, '"')]
}

func dialTicket(srv *httptest.Server, ticket string) (*websocket.Conn, *http.Response, error) {
	d := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	return d.Dial(wsURL(srv)+"?ticket="+ticket, http.Header{"Origin": {srv.URL}})
}

func TestWebSocketTicketIsSingleUse(t *testing.T) {
	e := newAuthEnv(t)
	srv, _ := e.server(t)

	ticket := mintTicket(t, srv, nil)
	conn, _, err := dialTicket(srv, ticket)
	if err != nil {
		t.Fatalf("first use must work: %v", err)
	}
	var line map[string]any
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := conn.ReadJSON(&line); err != nil {
		t.Fatal(err)
	}
	conn.Close()

	// Replay: the same URL again is refused before the upgrade.
	if c, resp, err := dialTicket(srv, ticket); err == nil {
		c.Close()
		t.Fatal("a used ticket must not open a second WebSocket")
	} else if resp == nil || resp.StatusCode != 401 {
		t.Fatalf("replay: want 401, got %v %v", resp, err)
	}
}

func TestWebSocketTicketRejections(t *testing.T) {
	e := newAuthEnv(t)
	srv, _ := e.server(t)
	for name, ticket := range map[string]string{
		"garbage": "ulwt_nonsense", "empty-looking": "x", "a bearer token": e.mint(t, service.ScopeAdmin, time.Hour),
	} {
		c, resp, err := dialTicket(srv, ticket)
		if err == nil {
			c.Close()
			t.Fatalf("%s must not open a WebSocket", name)
		}
		if resp == nil || resp.StatusCode != 401 {
			t.Fatalf("%s: want 401 (no fall-through to local), got %v", name, resp)
		}
	}
}

func TestWebSocketTicketExpires(t *testing.T) {
	e := newAuthEnv(t)
	srv, s := e.server(t)
	clock := time.Now()
	s.tickets().now = func() time.Time { return clock }

	ticket := mintTicket(t, srv, nil)
	clock = clock.Add(wsTicketTTL - time.Second)
	// Not consumed here: check expiry on a fresh one instead.
	t2 := mintTicket(t, srv, nil)
	clock = clock.Add(wsTicketTTL + time.Second) // both now past their TTL
	for _, tk := range []string{ticket, t2} {
		if c, resp, err := dialTicket(srv, tk); err == nil {
			c.Close()
			t.Fatal("an expired ticket must not work")
		} else if resp == nil || resp.StatusCode != 401 {
			t.Fatalf("expired: want 401, got %v", resp)
		}
	}
	// A ticket used within its TTL works.
	clock = clock.Add(0)
	fresh := mintTicket(t, srv, nil)
	clock = clock.Add(wsTicketTTL - time.Second)
	c, _, err := dialTicket(srv, fresh)
	if err != nil {
		t.Fatalf("ticket within TTL: %v", err)
	}
	c.Close()
}

func TestWebSocketTicketsOnlyWorkOnWebSocketRoutes(t *testing.T) {
	e := newAuthEnv(t)
	srv, _ := e.server(t)
	t.Setenv(envRequireToken, "1")
	ticket := mintTicket(t, srv, bearer(e.mint(t, service.ScopeRead, time.Hour)))
	// Strict mode: a ticket in the query of a plain REST route is not a credential.
	if resp, _ := get(t, srv.URL+"/api/v1/containers/abc/logs?ticket="+ticket, nil); resp.StatusCode != 401 {
		t.Fatalf("ticket on a REST route: want 401, got %d", resp.StatusCode)
	}
	// ... and the failed attempt did not burn it for its proper use.
	c, _, err := dialTicket(srv, ticket)
	if err != nil {
		t.Fatalf("ticket should still work on the WebSocket route: %v", err)
	}
	c.Close()
}

func TestStrictModeTicketsNeedAToken(t *testing.T) {
	e := newAuthEnv(t)
	srv, _ := e.server(t)
	t.Setenv(envRequireToken, "1")

	if resp, _ := reqWith(t, "POST", srv.URL+"/api/v1/ws-tickets", `{}`, withJSON(nil)); resp.StatusCode != 401 {
		t.Fatalf("strict mode must not mint tickets for anonymous callers: %d", resp.StatusCode)
	}
	// A token holder can mint one and use it (browser path with a token).
	ticket := mintTicket(t, srv, bearer(e.mint(t, service.ScopeRead, time.Hour)))
	c, _, err := dialTicket(srv, ticket)
	if err != nil {
		t.Fatalf("strict mode: ticket from a token holder should work: %v", err)
	}
	c.Close()
	// And a WebSocket with no credential at all is refused.
	if c, resp, err := dialTicket(srv, ""); err == nil {
		c.Close()
		t.Fatal("strict mode: anonymous WebSocket must be refused")
	} else if resp == nil || resp.StatusCode != 401 {
		t.Fatalf("anonymous WS in strict mode: want 401, got %v", resp)
	}
}

func TestTicketStoreUnit(t *testing.T) {
	ts := newTicketStore()
	now := time.Now()
	ts.now = func() time.Time { return now }
	p := principal{scope: service.ScopeRead, tokenID: "abc"}

	id, ttl, err := ts.Issue(p)
	if err != nil || ttl != wsTicketTTL || !strings.HasPrefix(id, "ulwt_") {
		t.Fatalf("issue: %q %v %v", id, ttl, err)
	}
	if id2, _, _ := ts.Issue(p); id2 == id {
		t.Fatal("tickets must be unique")
	}
	g, ok := ts.Redeem(id)
	if !ok || g.scope != service.ScopeRead || g.tokenID != "abc" {
		t.Fatalf("redeem: %+v %v", g, ok)
	}
	if _, ok := ts.Redeem(id); ok {
		t.Fatal("second redeem must fail")
	}

	// Concurrent redemption: exactly one winner.
	id, _, _ = ts.Issue(p)
	var wins int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := ts.Redeem(id); ok {
				atomic.AddInt32(&wins, 1)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d concurrent redemptions succeeded, want exactly 1", wins)
	}

	// Cap on outstanding tickets; expiry frees room.
	ts = newTicketStore()
	ts.now = func() time.Time { return now }
	for i := 0; i < wsTicketMax; i++ {
		if _, _, err := ts.Issue(p); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := ts.Issue(p); err != errTooManyTickets {
		t.Fatalf("want errTooManyTickets, got %v", err)
	}
	now = now.Add(wsTicketTTL + time.Second)
	if _, _, err := ts.Issue(p); err != nil {
		t.Fatalf("expired tickets should free room: %v", err)
	}
}

func TestTicketsNeverAppearInAccessLogs(t *testing.T) {
	for in, want := range map[string]string{
		"/api/v1/containers/x/logs/stream?ticket=ulwt_SECRET":                    "/api/v1/containers/x/logs/stream?ticket=REDACTED",
		"/api/v1/containers/x/logs/stream?since_ms=5&ticket=ulwt_SECRET&foo=bar": "/api/v1/containers/x/logs/stream?since_ms=5&ticket=REDACTED&foo=bar",
		"/api/v1/health": "/api/v1/health",
	} {
		if got := redactTicket(in); got != want {
			t.Fatalf("redactTicket(%q) = %q, want %q", in, got, want)
		}
	}
	line := redactingLogFormatter(gin.LogFormatterParams{
		Method: "GET", Path: "/x?ticket=ulwt_SECRET", StatusCode: 101, TimeStamp: time.Now(),
	})
	if strings.Contains(line, "ulwt_SECRET") || !strings.Contains(line, "REDACTED") {
		t.Fatalf("log line leaks the ticket: %s", line)
	}
}
