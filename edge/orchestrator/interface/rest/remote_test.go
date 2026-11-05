package rest

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/remote"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/assertion"
)

const testCluster = "cluster_remote1"

// cloudKeys is a stand-in for the cloud: one signing key and a key set that can
// be taken offline.
type cloudKeys struct {
	mu   sync.Mutex
	kid  string
	pub  ed25519.PublicKey
	sig  *assertion.Signer
	down bool
	jwks []assertion.JWK
}

func newCloudKeys(t *testing.T) *cloudKeys {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	kid := assertion.Thumbprint(pub)
	s, _ := assertion.NewSigner(kid, priv)
	now := time.Now()
	return &cloudKeys{kid: kid, pub: pub, sig: s,
		jwks: []assertion.JWK{assertion.NewJWK(kid, pub, now.Add(-time.Hour), now.Add(24*time.Hour))}}
}

func (c *cloudKeys) fetch(context.Context) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.down {
		return nil, errors.New("cloud down")
	}
	return json.Marshal(assertion.KeySet{Keys: c.jwks})
}

func (c *cloudKeys) mint(t *testing.T, mutate func(*assertion.Claims), scopes ...string) string {
	t.Helper()
	now := time.Now()
	cl := assertion.Claims{
		Issuer: assertion.DefaultIssuer, Subject: "acct_owner", Audience: testCluster, Scopes: scopes,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(assertion.DefaultTTL).Unix(), JTI: randID(),
	}
	if mutate != nil {
		mutate(&cl)
	}
	tok, err := c.sig.Sign(cl)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func randID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return string(append([]byte("jti-"), []byte(strings.TrimRight(hexOf(b), "="))...))
}

func hexOf(b []byte) string {
	const d = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, x := range b {
		out = append(out, d[x>>4], d[x&15])
	}
	return string(out)
}

type remoteEnv struct {
	cloud  *cloudKeys
	cache  *remote.KeyCache
	remote *httptest.Server
	local  *httptest.Server
	srv    *OrchestratorRESTServer
	ui     string
}

// newRemoteEnv starts the real remote router and the real local router over the
// same server value, so the tests can show the two chains differ.
func newRemoteEnv(t *testing.T, fetchKeys bool) *remoteEnv {
	t.Helper()
	t.Setenv(envAllowedOrigins, "")
	t.Setenv(envAllowedHosts, "")
	t.Setenv(envRequireToken, "")
	cloud := newCloudKeys(t)
	cache, err := remote.NewKeyCache(remote.KeyCacheConfig{Fetch: cloud.fetch, Path: filepath.Join(t.TempDir(), "keys.json")})
	if err != nil {
		t.Fatal(err)
	}
	if fetchKeys {
		if err := cache.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	ver, err := remote.NewVerifier(remote.VerifierConfig{Keys: cache, Audience: func() (string, error) { return testCluster, nil }})
	if err != nil {
		t.Fatal(err)
	}
	ui := filepath.Join(t.TempDir(), "ui")
	if err := os.MkdirAll(ui, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ui, "index.html"), []byte("<html>orchestrator-ui</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &OrchestratorRESTServer{logSrc: fakeLogs{}, Remote: ver}
	e := &remoteEnv{cloud: cloud, cache: cache, srv: s, ui: ui}
	e.remote = httptest.NewServer(s.RemoteHandler(apiBase, ui))
	e.local = httptest.NewServer(s.newRouter(apiBase, ui))
	t.Cleanup(e.remote.Close)
	t.Cleanup(e.local.Close)
	return e
}

func asr(tok string) map[string]string { return map[string]string{assertion.HeaderName: tok} }

func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func TestRemoteAcceptsAValidAssertion(t *testing.T) {
	e := newRemoteEnv(t, true)
	tok := e.cloud.mint(t, nil, "read")
	if resp, body := get(t, e.remote.URL+"/api/v1/health", asr(tok)); resp.StatusCode != 200 {
		t.Fatalf("health: %d %s", resp.StatusCode, body)
	}
	// The same UI and API handlers are served on the remote listener: one origin.
	if resp, body := get(t, e.remote.URL+"/", asr(e.cloud.mint(t, nil, "read"))); resp.StatusCode != 200 || !strings.Contains(body, "orchestrator-ui") {
		t.Fatalf("ui: %d %q", resp.StatusCode, body)
	}
	if resp, body := get(t, e.remote.URL+"/api/v1/containers/abc/logs", asr(e.cloud.mint(t, nil, "read"))); resp.StatusCode != 200 || !strings.Contains(body, `"hi"`) {
		t.Fatalf("logs: %d %q", resp.StatusCode, body)
	}
}

// Everything without a valid assertion is refused, including routes that are
// public on the local listener.
func TestRemoteRejectsAnythingWithoutAValidAssertion(t *testing.T) {
	e := newRemoteEnv(t, true)
	for _, path := range []string{"/api/v1/health", "/api/v1/containers", "/api/v1/deployments", "/", "/assets/x.js", "/api/v1/nope"} {
		for name, hdr := range map[string]map[string]string{
			"no credentials": nil,
			"empty header":   {assertion.HeaderName: ""},
			"garbage":        {assertion.HeaderName: "not.a.jwt"},
			"bearer instead": {"Authorization": "Bearer " + e.cloud.mint(t, nil, "admin")},
			"subject header": {"X-Subject-Id": "cluster_remote1"},
			"forwarded":      {"X-Forwarded-For": "127.0.0.1", "X-Real-IP": "127.0.0.1"},
		} {
			resp, _ := get(t, e.remote.URL+path, hdr)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("%s %s: status %d, want 401", name, path, resp.StatusCode)
			}
		}
	}
	// Mutating verbs too.
	resp, _ := reqWith(t, "POST", e.remote.URL+"/api/v1/deployments", `{"source":"gh:a/b"}`, map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("POST without assertion: %d", resp.StatusCode)
	}
}

func TestRemoteRejectsInvalidAssertions(t *testing.T) {
	e := newRemoteEnv(t, true)
	other := newCloudKeys(t)
	mutate := func(f func(*assertion.Claims)) string { return e.cloud.mint(t, f, "read") }
	cases := map[string]string{
		"wrong audience": mutate(func(c *assertion.Claims) { c.Audience = "cluster_someone_else" }),
		"expired": mutate(func(c *assertion.Claims) {
			c.IssuedAt = time.Now().Add(-10 * time.Minute).Unix()
			c.ExpiresAt = time.Now().Add(-9 * time.Minute).Unix()
		}),
		"unknown kid":    other.mint(t, nil, "read"),
		"no scope":       e.cloud.mint(t, nil),
		"unknown scope":  e.cloud.mint(t, nil, "root"),
		"wrong issuer":   mutate(func(c *assertion.Claims) { c.Issuer = "attacker" }),
		"lifetime":       mutate(func(c *assertion.Claims) { c.ExpiresAt = time.Now().Add(time.Hour).Unix() }),
		"tampered claim": strings.Replace(e.cloud.mint(t, nil, "read"), ".", ".x", 1),
	}
	for name, tok := range cases {
		resp, body := get(t, e.remote.URL+"/api/v1/health", asr(tok))
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401 (%s)", name, resp.StatusCode, body)
		}
		if strings.Contains(body, "audience") || strings.Contains(body, "kid") {
			t.Errorf("%s: response leaks the reason: %s", name, body)
		}
	}
}

func TestRemoteRejectsReplayedAssertion(t *testing.T) {
	e := newRemoteEnv(t, true)
	tok := e.cloud.mint(t, nil, "read")
	if resp, _ := get(t, e.remote.URL+"/api/v1/health", asr(tok)); resp.StatusCode != 200 {
		t.Fatalf("first use: %d", resp.StatusCode)
	}
	if resp, _ := get(t, e.remote.URL+"/api/v1/health", asr(tok)); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replay: %d, want 401", resp.StatusCode)
	}
}

func TestRemoteFailsClosedWithNoKeyEver(t *testing.T) {
	e := newRemoteEnv(t, false) // key set never fetched
	e.cloud.mu.Lock()
	e.cloud.down = true // and the cloud is down
	e.cloud.mu.Unlock()
	if resp, _ := get(t, e.remote.URL+"/api/v1/health", asr(e.cloud.mint(t, nil, "admin"))); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no key: %d, want 401", resp.StatusCode)
	}
}

func TestRemoteScopesUseTheSameRouteTable(t *testing.T) {
	e := newRemoteEnv(t, true)
	// read: GETs allowed, deploying is not.
	read := func() map[string]string { return asr(e.cloud.mint(t, nil, "read")) }
	if resp, _ := reqWith(t, "POST", e.remote.URL+"/api/v1/deployments", `{"source":"gh:a/b"}`, merge(read(), map[string]string{"Content-Type": "application/json"})); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("read-scoped assertion on POST /deployments: %d, want 403", resp.StatusCode)
	}
	if resp, _ := reqWith(t, "POST", e.remote.URL+"/api/v1/ws-tickets", `{}`, merge(read(), map[string]string{"Content-Type": "application/json"})); resp.StatusCode != 200 {
		t.Fatalf("read-scoped assertion on POST /ws-tickets: %d, want 200", resp.StatusCode)
	}
	// deploy passes the scope check (the handler then rejects the empty body,
	// proving we got past authorization without needing a real service).
	dep := merge(asr(e.cloud.mint(t, nil, "deploy")), map[string]string{"Content-Type": "application/json"})
	if resp, _ := reqWith(t, "POST", e.remote.URL+"/api/v1/deployments", `{}`, dep); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("deploy-scoped assertion on POST /deployments: %d, want 400 from the handler", resp.StatusCode)
	}
}

func TestRemoteRouteMissingFromTheTableRequiresAdmin(t *testing.T) {
	// accessFor is the single lookup both listeners use.
	if a := accessFor("DELETE", "/api/v1/something-new", "/api/v1"); a.kind != routeScoped || a.scope != service.ScopeAdmin {
		t.Fatalf("unlisted route access = %+v, want admin", a)
	}
	if a := accessFor("POST", "/api/v1/deployments", "/api/v1"); a.scope != service.ScopeDeploy {
		t.Fatalf("POST /deployments = %+v", a)
	}
}

func TestRemoteIgnoresLocalCredentialsAndTickets(t *testing.T) {
	e := newRemoteEnv(t, true)
	// A valid assertion plus junk Authorization is judged by the assertion only.
	h := merge(asr(e.cloud.mint(t, nil, "read")), map[string]string{"Authorization": "Bearer ulat_bogus"})
	if resp, _ := get(t, e.remote.URL+"/api/v1/health", h); resp.StatusCode != 200 {
		t.Fatalf("junk Authorization changed the outcome: %d", resp.StatusCode)
	}
	// Two assertion headers are ambiguous and refused.
	req, _ := http.NewRequest("GET", e.remote.URL+"/api/v1/health", nil)
	req.Header.Add(assertion.HeaderName, e.cloud.mint(t, nil, "read"))
	req.Header.Add(assertion.HeaderName, e.cloud.mint(t, nil, "read"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("two assertion headers: %d, want 401", resp.StatusCode)
	}
}

func TestRemoteBodiesMustStillBeJSON(t *testing.T) {
	e := newRemoteEnv(t, true)
	resp, _ := reqWith(t, "POST", e.remote.URL+"/api/v1/ws-tickets", `x`, merge(asr(e.cloud.mint(t, nil, "read")), map[string]string{"Content-Type": "text/plain"}))
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestRemoteWebSocketNeedsAnAssertionOnTheUpgrade(t *testing.T) {
	e := newRemoteEnv(t, true)
	if c, resp, err := dial(e.remote, nil); err == nil {
		c.Close()
		t.Fatal("WebSocket upgrade without assertion succeeded")
	} else if resp == nil || resp.StatusCode != 401 {
		t.Fatalf("want 401, got %v (%v)", resp, err)
	}
	// With one, the cloud-side Origin (not ours) is fine: the cloud vouched.
	conn, _, err := dial(e.remote, http.Header{
		assertion.HeaderName: {e.cloud.mint(t, nil, "read")},
		"Origin":             {"https://cockpit.example"},
	})
	if err != nil {
		t.Fatalf("authenticated upgrade refused: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var line struct{ Message string }
	if err := conn.ReadJSON(&line); err != nil || line.Message != "line one" {
		t.Fatalf("got %+v %v", line, err)
	}
	// A read-only assertion cannot reach... a ticketed stream either: tickets are not credentials here.
	d := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	if c, resp, err := d.Dial(wsURL(e.remote)+"?ticket=ulwt_anything", nil); err == nil {
		c.Close()
		t.Fatal("ticket accepted as a credential on the remote listener")
	} else if resp == nil || resp.StatusCode != 401 {
		t.Fatalf("want 401, got %v (%v)", resp, err)
	}
}

func TestRemoteNotConfiguredIs503NotOpen(t *testing.T) {
	s := &OrchestratorRESTServer{logSrc: fakeLogs{}}
	srv := httptest.NewServer(s.RemoteHandler(apiBase, ""))
	defer srv.Close()
	if resp, _ := get(t, srv.URL+"/api/v1/health", asr("anything")); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", resp.StatusCode)
	}
}

// --- the local listener never accepts them ---------------------------------

func TestLoopbackListenerRejectsAssertionsAndForwardedIdentity(t *testing.T) {
	e := newRemoteEnv(t, true)
	valid := e.cloud.mint(t, nil, "admin")
	for name, hdr := range map[string]map[string]string{
		"valid assertion":     asr(valid),
		"garbage assertion":   asr("x"),
		"empty assertion":     {assertion.HeaderName: ""},
		"x-subject-id":        {"X-Subject-Id": testCluster},
		"assertion + subject": {assertion.HeaderName: valid, "X-Subject-Id": testCluster},
		"lowercase header":    {"x-underleaf-assertion": valid},
		"assertion on a POST": merge(asr(valid), map[string]string{"Content-Type": "application/json"}),
	} {
		for _, path := range []string{"/api/v1/health", "/api/v1/containers", "/", "/api/v1/ws-tickets"} {
			method := "GET"
			if path == "/api/v1/ws-tickets" {
				method = "POST"
				hdr = merge(hdr, map[string]string{"Content-Type": "application/json"})
			}
			resp, _ := reqWith(t, method, e.local.URL+path, "", hdr)
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("%s %s %s: status %d, want 403", name, method, path, resp.StatusCode)
			}
		}
	}
	// ...but the local listener is otherwise untouched.
	if resp, _ := get(t, e.local.URL+"/api/v1/health", nil); resp.StatusCode != 200 {
		t.Fatalf("local health: %d", resp.StatusCode)
	}
	// Presenting the assertion as a bearer token is no credential either.
	if resp, _ := get(t, e.local.URL+"/api/v1/containers", map[string]string{"Authorization": "Bearer " + valid}); resp.StatusCode == 200 {
		t.Fatalf("assertion accepted as a bearer token on the local listener")
	}
}

func TestLoopbackListenerRejectsAssertionsInStrictModeToo(t *testing.T) {
	e := newRemoteEnv(t, true)
	t.Setenv(envRequireToken, "1")
	if resp, _ := get(t, e.local.URL+"/api/v1/containers", asr(e.cloud.mint(t, nil, "admin"))); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("got %d, want 403", resp.StatusCode)
	}
}

// A remote router never falls back to the local principal, and a local router
// ignores its Remote verifier: the two cannot be mistaken for each other.
func TestListenersDoNotShareAuthenticationPaths(t *testing.T) {
	e := newRemoteEnv(t, true)
	// local: no credential -> local principal -> 200 (default install).
	if resp, _ := get(t, e.local.URL+"/api/v1/containers/abc/logs", nil); resp.StatusCode != 200 {
		t.Fatalf("local without credentials: %d", resp.StatusCode)
	}
	// remote: the same request -> 401.
	if resp, _ := get(t, e.remote.URL+"/api/v1/containers/abc/logs", nil); resp.StatusCode != 401 {
		t.Fatalf("remote without credentials: %d", resp.StatusCode)
	}
}
