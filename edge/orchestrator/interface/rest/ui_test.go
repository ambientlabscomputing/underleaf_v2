package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

type fakeLogs struct{}

func (fakeLogs) GetLogs(context.Context, string, int64, int64, int, int64) (*service.LogPage, error) {
	return &service.LogPage{Lines: []*types.LogLine{{DockerID: "abc", Message: "hi"}}}, nil
}

func (fakeLogs) StreamLogs(_ context.Context, id string, _ int64) (<-chan *types.LogLine, error) {
	ch := make(chan *types.LogLine, 2)
	ch <- &types.LogLine{DockerID: id, Message: "line one"}
	ch <- &types.LogLine{DockerID: id, Message: "line two"}
	close(ch)
	return ch, nil
}

const apiBase = "/api/v1/"

// newUITestServer starts the real router (guards, API routes, UI) on a
// loopback httptest server with a fake UI build in a temp dir. A file outside
// the UI root holds a secret that no request may ever return.
func newUITestServer(t *testing.T) *httptest.Server {
	t.Helper()
	t.Setenv(envAllowedOrigins, "")
	t.Setenv(envAllowedHosts, "")
	root := t.TempDir()
	ui := filepath.Join(root, "ui")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(ui, "assets"), 0o755))
	must(os.WriteFile(filepath.Join(ui, "index.html"), []byte("<html>orchestrator-ui</html>"), 0o644))
	must(os.WriteFile(filepath.Join(ui, "assets", "app-123.js"), []byte("console.log('ui')"), 0o644))
	must(os.WriteFile(filepath.Join(ui, ".env"), []byte("DOTFILE-SECRET"), 0o644))
	must(os.WriteFile(filepath.Join(root, "secret.txt"), []byte("OUTSIDE-SECRET"), 0o644))

	s := &OrchestratorRESTServer{logSrc: fakeLogs{}}
	srv := httptest.NewServer(s.newRouter(apiBase, ui))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	return reqWith(t, "GET", url, "", hdr)
}

func reqWith(t *testing.T, method, url, body string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestUIAndAPIShareOneOrigin(t *testing.T) {
	srv := newUITestServer(t)

	// The UI and the API answer on the very same host:port.
	resp, body := get(t, srv.URL+"/", nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "orchestrator-ui") {
		t.Fatalf("UI at /: %d %q", resp.StatusCode, body)
	}
	resp, body = get(t, srv.URL+"/api/v1/health", nil)
	if resp.StatusCode != 200 || !strings.Contains(body, `"OK"`) {
		t.Fatalf("API on the UI origin: %d %q", resp.StatusCode, body)
	}

	// Client-side routes survive a reload (SPA fallback); assets are served as is.
	if resp, body = get(t, srv.URL+"/containers/abc", nil); resp.StatusCode != 200 || !strings.Contains(body, "orchestrator-ui") {
		t.Fatalf("SPA fallback: %d %q", resp.StatusCode, body)
	}
	resp, body = get(t, srv.URL+"/assets/app-123.js", nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "console.log") || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d %q %v", resp.StatusCode, body, resp.Header)
	}
	if resp, _ = get(t, srv.URL+"/", nil); resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("index.html must not be cached: %v", resp.Header)
	}

	// Unknown API paths are API 404s, never the SPA.
	resp, body = get(t, srv.URL+"/api/v1/nope", nil)
	if resp.StatusCode != 404 || strings.Contains(body, "orchestrator-ui") || !strings.Contains(resp.Header.Get("Content-Type"), "json") {
		t.Fatalf("unknown API path: %d %q", resp.StatusCode, body)
	}
}

func TestNoCORSHeadersAreEverSent(t *testing.T) {
	srv := newUITestServer(t)
	for _, origin := range []string{"", srv.URL, "http://localhost:5183"} {
		hdr := map[string]string{}
		if origin != "" {
			hdr["Origin"] = origin
		}
		resp, _ := get(t, srv.URL+"/api/v1/health", hdr)
		for k := range resp.Header {
			if strings.HasPrefix(strings.ToLower(k), "access-control-") {
				t.Fatalf("origin %q: unexpected CORS header %s", origin, k)
			}
		}
	}
	// A cross-origin preflight is refused outright, not answered.
	resp, _ := reqWith(t, "OPTIONS", srv.URL+"/api/v1/deployments", "", map[string]string{
		"Origin": "https://evil.example", "Access-Control-Request-Method": "POST",
	})
	if resp.StatusCode != 403 || resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("foreign preflight: %d %v", resp.StatusCode, resp.Header)
	}
}

func TestUIAssetsAreBehindTheSameGuards(t *testing.T) {
	srv := newUITestServer(t)
	json := map[string]string{"Content-Type": "application/json"}

	// DNS rebinding: a rebound hostname gets neither assets nor API.
	for _, path := range []string{"/", "/assets/app-123.js", "/api/v1/health"} {
		if resp, _ := get(t, srv.URL+path, map[string]string{"Host": "evil.example"}); resp.StatusCode != 403 {
			t.Fatalf("%s with a rebound Host: got %d, want 403", path, resp.StatusCode)
		}
	}
	// Foreign Origin is refused on assets too.
	if resp, _ := get(t, srv.URL+"/", map[string]string{"Origin": "https://evil.example"}); resp.StatusCode != 403 {
		t.Fatalf("foreign Origin on UI: got %d, want 403", resp.StatusCode)
	}
	// Content-Type guard unchanged: a form POST to the API is 415, even same-origin.
	resp, _ := reqWith(t, "POST", srv.URL+"/api/v1/deployments", "source=x", map[string]string{
		"Content-Type": "application/x-www-form-urlencoded", "Origin": srv.URL})
	if resp.StatusCode != 415 {
		t.Fatalf("form POST: got %d, want 415", resp.StatusCode)
	}
	// A same-origin JSON POST passes the guards and reaches the handler (400: body invalid).
	h := map[string]string{"Content-Type": "application/json", "Origin": srv.URL}
	if resp, _ = reqWith(t, "POST", srv.URL+"/api/v1/deployments", `{}`, h); resp.StatusCode != 400 {
		t.Fatalf("same-origin JSON POST should reach the handler (400), got %d", resp.StatusCode)
	}
	// A sibling port on the same host is a different origin.
	h["Origin"] = "http://127.0.0.1:1"
	if resp, _ = reqWith(t, "POST", srv.URL+"/api/v1/deployments", `{}`, h); resp.StatusCode != 403 {
		t.Fatalf("different-port origin: got %d, want 403", resp.StatusCode)
	}
	// Non-GET on a UI path is not served.
	if resp, _ = reqWith(t, "POST", srv.URL+"/", `{}`, json); resp.StatusCode != 405 {
		t.Fatalf("POST to UI path: got %d, want 405", resp.StatusCode)
	}
}

func TestUIDoesNotServeFilesOutsideItsRoot(t *testing.T) {
	srv := newUITestServer(t)
	for _, p := range []string{"/.env", "/../secret.txt", "/%2e%2e/secret.txt", "/assets/../../secret.txt", "/..%2fsecret.txt", "/assets/%2e%2e/%2e%2e/secret.txt"} {
		resp, body := get(t, srv.URL+p, nil)
		if strings.Contains(body, "OUTSIDE-SECRET") || strings.Contains(body, "DOTFILE-SECRET") {
			t.Fatalf("%s leaked a file: %d %q", p, resp.StatusCode, body)
		}
	}
	// Security headers on everything.
	resp, _ := get(t, srv.URL+"/", nil)
	if resp.Header.Get("X-Frame-Options") != "DENY" || resp.Header.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("missing security headers: %v", resp.Header)
	}
}

func TestAPIOnlyWhenNoUIConfigured(t *testing.T) {
	t.Setenv(envAllowedOrigins, "")
	s := &OrchestratorRESTServer{logSrc: fakeLogs{}}
	srv := httptest.NewServer(s.newRouter(apiBase, ""))
	defer srv.Close()
	if resp, _ := get(t, srv.URL+"/", nil); resp.StatusCode != 404 {
		t.Fatalf("no UI dir: / should be 404, got %d", resp.StatusCode)
	}
	if resp, _ := get(t, srv.URL+"/api/v1/health", nil); resp.StatusCode != 200 {
		t.Fatalf("API must still be served: %d", resp.StatusCode)
	}
}

func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/containers/abc/logs/stream"
}

func dial(srv *httptest.Server, hdr http.Header) (*websocket.Conn, *http.Response, error) {
	d := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	return d.Dial(wsURL(srv), hdr)
}

func TestLogWebSocketWorksOnTheUIOrigin(t *testing.T) {
	srv := newUITestServer(t)

	// The browser's view: Origin is the page's own origin (same as the API's).
	conn, _, err := dial(srv, http.Header{"Origin": {srv.URL}})
	if err != nil {
		t.Fatalf("same-origin WebSocket upgrade refused: %v", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for _, want := range []string{"line one", "line two"} {
		var line types.LogLine
		if err := conn.ReadJSON(&line); err != nil {
			t.Fatal(err)
		}
		if line.Message != want || line.DockerID != "abc" {
			t.Fatalf("got %+v, want %q", line, want)
		}
	}

	// A non-browser client (no Origin) still works; so does the dev origin via the allowlist.
	if c, _, err := dial(srv, nil); err != nil {
		t.Fatalf("no-Origin client: %v", err)
	} else {
		c.Close()
	}
	if c, _, err := dial(srv, http.Header{"Origin": {"http://localhost:5183"}}); err != nil {
		t.Fatalf("dev origin: %v", err)
	} else {
		c.Close()
	}
}

func TestLogWebSocketStillRejectsForeignOriginsAndHosts(t *testing.T) {
	srv := newUITestServer(t)
	cases := map[string]http.Header{
		"foreign origin":            {"Origin": {"https://evil.example"}},
		"same host, other port":     {"Origin": {"http://127.0.0.1:1"}},
		"rebound host":              {"Host": {"evil.example"}},
		"rebound host, own origin":  {"Host": {"evil.example"}, "Origin": {"http://evil.example"}},
		"rebound host, fake origin": {"Host": {"evil.example"}, "Origin": {srv.URL}},
	}
	for name, hdr := range cases {
		conn, resp, err := dial(srv, hdr)
		if err == nil {
			conn.Close()
			t.Fatalf("%s: upgrade must be refused", name)
		}
		if resp == nil || resp.StatusCode != 403 {
			t.Fatalf("%s: want HTTP 403, got %v (%v)", name, resp, err)
		}
	}
}

func TestLogRESTRouteAndJSON(t *testing.T) {
	srv := newUITestServer(t)
	resp, body := get(t, srv.URL+"/api/v1/containers/abc/logs", nil)
	var page struct {
		Lines []types.LogLine `json:"lines"`
	}
	if resp.StatusCode != 200 || json.Unmarshal([]byte(body), &page) != nil || len(page.Lines) != 1 {
		t.Fatalf("logs: %d %q", resp.StatusCode, body)
	}
}
