package rest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func guardedRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(requestGuard())
	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }
	r.GET("/x", ok)
	r.POST("/x", ok)
	return r
}

func do(r http.Handler, method, host string, headers map[string]string, body string) int {
	req := httptest.NewRequest(method, "/x", strings.NewReader(body))
	req.Host = host
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestRequestGuard(t *testing.T) {
	t.Setenv(envAllowedOrigins, "")
	t.Setenv(envAllowedHosts, "")
	r := guardedRouter()
	js := map[string]string{"Content-Type": "application/json"}

	cases := []struct {
		name    string
		method  string
		host    string
		headers map[string]string
		want    int
	}{
		{"cli/curl: no origin, json", "POST", "localhost:9090", js, 200},
		{"json content-type with charset", "POST", "127.0.0.1:9090", map[string]string{"Content-Type": "application/json; charset=utf-8"}, 200},
		{"ipv6 loopback host", "GET", "[::1]:9090", nil, 200},
		{"ui dev origin", "POST", "localhost:9090", map[string]string{"Content-Type": "application/json", "Origin": "http://localhost:5183"}, 200},
		{"cross-site origin GET", "GET", "localhost:9090", map[string]string{"Origin": "https://evil.example"}, 403},
		{"cross-site origin POST", "POST", "localhost:9090", map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example"}, 403},
		{"csrf via simple request (text/plain)", "POST", "localhost:9090", map[string]string{"Content-Type": "text/plain"}, 415},
		{"csrf via form post", "POST", "localhost:9090", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, 415},
		{"post with no content-type", "POST", "localhost:9090", nil, 415},
		{"dns rebinding host", "GET", "evil.example:9090", nil, 403},
		{"dns rebinding host, allowed-looking origin", "POST", "evil.example", map[string]string{"Content-Type": "application/json", "Origin": "http://localhost:5183"}, 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := do(r, tc.method, tc.host, tc.headers, `{}`); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRequestGuardEnvExtensions(t *testing.T) {
	t.Setenv(envAllowedOrigins, "https://ui.internal, http://localhost:9090")
	t.Setenv(envAllowedHosts, "orch.internal")
	r := guardedRouter()

	if got := do(r, "GET", "orch.internal:9090", map[string]string{"Origin": "https://ui.internal"}, ""); got != 200 {
		t.Fatalf("configured host+origin: got %d, want 200", got)
	}
	if got := do(r, "GET", "other.internal", nil, ""); got != 403 {
		t.Fatalf("unconfigured host: got %d, want 403", got)
	}
}

func TestWebSocketOriginCheck(t *testing.T) {
	t.Setenv(envAllowedOrigins, "")
	check := func(origin string) bool {
		req := httptest.NewRequest("GET", "/ws", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		return wsUpgrader.CheckOrigin(req)
	}
	if !check("") {
		t.Fatal("non-browser client (no Origin) should be allowed")
	}
	if !check("http://localhost:5183") {
		t.Fatal("UI origin should be allowed")
	}
	if check("https://evil.example") {
		t.Fatal("foreign origin must be rejected")
	}
}
