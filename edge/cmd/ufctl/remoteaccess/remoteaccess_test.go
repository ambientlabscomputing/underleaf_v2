package remoteaccess

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func isolate(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ufctl-home")
	t.Setenv(EnvHome, dir)
	return dir
}

// --- store -----------------------------------------------------------------

func TestStoreIsOwnerOnly(t *testing.T) {
	dir := isolate(t)
	if err := Save(Credentials{CloudURL: "https://c.example/api", DeviceKeyID: "dk_1", DeviceKey: "secret", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	di, _ := os.Stat(dir)
	fi, _ := os.Stat(filepath.Join(dir, "device.json"))
	if di.Mode().Perm() != 0o700 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("dir %o file %o, want 700 / 600", di.Mode().Perm(), fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("stray files left behind: %v", entries)
	}
	c, err := Load()
	if err != nil || c.DeviceKey != "secret" {
		t.Fatalf("Load = %+v, %v", c, err)
	}
	// Overwriting keeps the mode.
	_ = Save(Credentials{CloudURL: "https://c.example/api", DeviceKey: "second"})
	fi, _ = os.Stat(filepath.Join(dir, "device.json"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode after overwrite %o", fi.Mode().Perm())
	}
}

func TestLoadRefusesLooseOrBrokenFiles(t *testing.T) {
	dir := isolate(t)
	if _, err := Load(); err != ErrNotLoggedIn {
		t.Fatalf("got %v, want ErrNotLoggedIn", err)
	}
	_ = os.MkdirAll(dir, 0o700)
	p := filepath.Join(dir, "device.json")
	_ = os.WriteFile(p, []byte(`{"cloud_url":"https://c","device_key":"k"}`), 0o644)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "accessible to other users") {
		t.Fatalf("world-readable key file: got %v", err)
	}
	_ = os.Chmod(p, 0o600)
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(p, []byte(`{not json`), 0o600)
	if _, err := Load(); err == nil {
		t.Fatal("corrupt file accepted")
	}
	_ = os.WriteFile(p, []byte(`{"cloud_url":"https://c"}`), 0o600)
	if _, err := Load(); err == nil {
		t.Fatal("file without a key accepted")
	}
}

func TestLogoutDeletes(t *testing.T) {
	isolate(t)
	_ = Save(Credentials{CloudURL: "https://c", DeviceKey: "k"})
	if err := Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err != ErrNotLoggedIn {
		t.Fatalf("got %v", err)
	}
	if err := Delete(); err != nil { // idempotent
		t.Fatal(err)
	}
}

// --- login -----------------------------------------------------------------

type fakeLoginCloud struct {
	mu     sync.Mutex
	script []string // token endpoint answers, in order: error codes, or "ok"
	polls  int
	device string
}

func (f *fakeLoginCloud) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ufctl/device", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.device = req["device_name"]
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code": "dc_1", "user_code": "ABCD-EFGH",
			"verification_uri":          "https://c.example/device",
			"verification_uri_complete": "https://c.example/device?code=ABCD-EFGH",
			"expires_in":                600, "interval": 1,
		})
	})
	mux.HandleFunc("/api/ufctl/token", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		step := f.script[min(f.polls, len(f.script)-1)]
		f.polls++
		f.mu.Unlock()
		if req["device_code"] != "dc_1" || req["grant_type"] != deviceGrant {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		if step == "ok" {
			_ = json.NewEncoder(w).Encode(map[string]string{"device_key_id": "dk_9", "device_key": "uldk_secret"})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": step})
	})
	return mux
}

func runLogin(t *testing.T, script ...string) (error, string, *fakeLoginCloud, []time.Duration) {
	t.Helper()
	f := &fakeLoginCloud{script: script}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	var out strings.Builder
	var sleeps []time.Duration
	clk := time.Now()
	err := Login(context.Background(), LoginOptions{
		CloudURL: srv.URL + "/api", DeviceName: "laptop", Out: &out,
		Sleep: func(_ context.Context, d time.Duration) error {
			sleeps = append(sleeps, d)
			clk = clk.Add(d)
			return nil
		},
		Now: func() time.Time { return clk },
	})
	return err, out.String(), f, sleeps
}

func TestLoginDeviceFlow(t *testing.T) {
	dir := isolate(t)
	err, out, f, sleeps := runLogin(t, "authorization_pending", "slow_down", "authorization_pending", "ok")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "https://c.example/device?code=ABCD-EFGH") || !strings.Contains(out, "ABCD-EFGH") || !strings.Contains(out, "Logged in") {
		t.Fatalf("output: %q", out)
	}
	if f.device != "laptop" || f.polls != 4 {
		t.Fatalf("device=%q polls=%d", f.device, f.polls)
	}
	// slow_down adds 5s to the polling interval from then on
	if len(sleeps) != 4 || sleeps[0] != time.Second || sleeps[2] != 6*time.Second {
		t.Fatalf("sleeps = %v", sleeps)
	}
	fi, _ := os.Stat(filepath.Join(dir, "device.json"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("device key mode %o", fi.Mode().Perm())
	}
	c, err := Load()
	if err != nil || c.DeviceKey != "uldk_secret" || c.DeviceKeyID != "dk_9" || !strings.HasSuffix(c.CloudURL, "/api") {
		t.Fatalf("stored = %+v %v", c, err)
	}
	if strings.Contains(out, "uldk_secret") {
		t.Fatal("the device key was printed")
	}
}

func TestLoginFailuresStoreNothing(t *testing.T) {
	for _, tc := range []struct{ code, want string }{
		{"access_denied", "denied"},
		{"expired_token", "expired"},
		{"server_error", "failed"},
	} {
		isolate(t)
		err, _, _, _ := runLogin(t, "authorization_pending", tc.code)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: got %v", tc.code, err)
		}
		if _, err := Load(); err != ErrNotLoggedIn {
			t.Fatalf("%s: a key was stored: %v", tc.code, err)
		}
	}
}

func TestLoginTimesOutWhenNeverApproved(t *testing.T) {
	isolate(t)
	err, _, _, _ := runLogin(t, "authorization_pending") // forever pending
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("got %v", err)
	}
}

func TestLoginRefusesInsecureCloudURLs(t *testing.T) {
	isolate(t)
	for _, u := range []string{"http://cloud.example/api", "ftp://x", "not a url", "https://"} {
		if err := Login(context.Background(), LoginOptions{CloudURL: u}); err == nil {
			t.Errorf("%q accepted", u)
		}
	}
	for _, u := range []string{"https://cloud.example/api", "http://localhost:8080/api", "http://127.0.0.1:1/api"} {
		if _, err := CheckCloudURL(u); err != nil {
			t.Errorf("%q refused: %v", u, err)
		}
	}
}

func TestLoginStopsOnContextCancel(t *testing.T) {
	isolate(t)
	f := &fakeLoginCloud{script: []string{"authorization_pending"}}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Login(ctx, LoginOptions{CloudURL: srv.URL + "/api"}); err == nil {
		t.Fatal("login ignored cancellation")
	}
}

// --- connect ---------------------------------------------------------------

type upstream struct {
	mu   sync.Mutex
	reqs []*http.Request
	srv  *httptest.Server
}

func newUpstream(t *testing.T, h http.HandlerFunc) *upstream {
	u := &upstream{}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.reqs = append(u.reqs, r)
		u.mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func (u *upstream) last() *http.Request {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.reqs) == 0 {
		return nil
	}
	return u.reqs[len(u.reqs)-1]
}

func (u *upstream) count() int { u.mu.Lock(); defer u.mu.Unlock(); return len(u.reqs) }

func proxyFor(t *testing.T, up *upstream) (*httptest.Server, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	h, err := NewProxyHandler(Credentials{CloudURL: up.srv.URL + "/api/v2/cloud", DeviceKey: "uldk_secret"}, "cluster_abc", port, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(h)
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, port
}

func do(t *testing.T, method, url string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, nil)
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

func TestProxyForwardsToTheClusterRemotePathWithOnlyTheDeviceKey(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "cloud", Value: "x"})
		_, _ = w.Write([]byte("ok"))
	})
	px, port := proxyFor(t, up)
	origin := "http://localhost:" + itoa(port)
	resp, body := do(t, "GET", px.URL+"/api/v1/containers?x=1&y=a%20b", map[string]string{
		"Host":                  "localhost:" + itoa(port),
		"Origin":                origin,
		"Authorization":         "Bearer browser-supplied",
		"Cookie":                "session=abc",
		"X-Underleaf-Assertion": "forged",
		"X-Subject-Id":          "cluster_abc",
		"X-Forwarded-For":       "1.2.3.4",
		"Referer":               origin + "/",
		"Accept":                "application/json",
	})
	if resp.StatusCode != 200 || body != "ok" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	r := up.last()
	if r.URL.Path != "/api/v2/cloud/clusters/cluster_abc/remote/api/v1/containers" || r.URL.RawQuery != "x=1&y=a%20b" {
		t.Fatalf("upstream got %s?%s", r.URL.Path, r.URL.RawQuery)
	}
	if r.Header.Get("Authorization") != "Bearer uldk_secret" {
		t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
	}
	for _, h := range []string{"Cookie", "X-Underleaf-Assertion", "X-Subject-Id", "Origin", "Referer", "X-Forwarded-Host"} {
		if v := r.Header.Get(h); v != "" {
			t.Errorf("%s leaked upstream: %q", h, v)
		}
	}
	if r.Host != strings.TrimPrefix(up.srv.URL, "http://") {
		t.Errorf("upstream Host = %q", r.Host)
	}
	if r.Header.Get("Accept") != "application/json" {
		t.Error("ordinary headers must pass through")
	}
	if resp.Header.Get("Set-Cookie") != "" {
		t.Error("cloud cookie leaked to localhost")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestProxyRefusesForeignHostsOriginsAndPaths(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	px, port := proxyFor(t, up)
	own := "localhost:" + itoa(port)
	cases := []struct {
		name string
		path string
		hdr  map[string]string
		want int
	}{
		{"rebound host", "/", map[string]string{"Host": "evil.example"}, 403},
		{"rebound host with port", "/", map[string]string{"Host": "evil.example:" + itoa(port)}, 403},
		{"other local port", "/", map[string]string{"Host": "localhost:1"}, 403},
		{"foreign origin", "/api/v1/deployments", map[string]string{"Host": own, "Origin": "https://evil.example"}, 403},
		{"other localhost port origin", "/", map[string]string{"Host": own, "Origin": "http://localhost:1"}, 403},
		{"https origin", "/", map[string]string{"Host": own, "Origin": "https://" + own}, 403},
		{"cross-site fetch", "/", map[string]string{"Host": own, "Sec-Fetch-Site": "cross-site"}, 403},
		{"same-site fetch", "/", map[string]string{"Host": own, "Sec-Fetch-Site": "same-site"}, 403},
		{"dot-dot", "/a/../../../../other", map[string]string{"Host": own}, 400},
		{"encoded slash", "/a%2f..%2fb", map[string]string{"Host": own}, 400},
		{"encoded dot", "/%2e%2e/x", map[string]string{"Host": own}, 400},
	}
	for _, tc := range cases {
		before := up.count()
		req, _ := http.NewRequest("GET", px.URL+tc.path, nil)
		req.Host = tc.hdr["Host"]
		for k, v := range tc.hdr {
			if k != "Host" {
				req.Header.Set(k, v)
			}
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s: status %d, want %d", tc.name, resp.StatusCode, tc.want)
		}
		if up.count() != before {
			t.Errorf("%s: the request reached the cloud", tc.name)
		}
	}
	// ...while the same-origin requests a browser makes are fine.
	for _, hdr := range []map[string]string{
		{"Host": own, "Origin": "http://" + own, "Sec-Fetch-Site": "same-origin"},
		{"Host": "127.0.0.1:" + itoa(port)},
		{"Host": own, "Sec-Fetch-Site": "none"},
	} {
		if resp, _ := do(t, "GET", px.URL+"/", hdr); resp.StatusCode != 200 {
			t.Errorf("%v: status %d", hdr, resp.StatusCode)
		}
	}
}

func TestProxyPassesCloudAuthFailuresAndHidesTheKeyOnErrors(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unauthorized", 401) })
	px, port := proxyFor(t, up)
	resp, _ := do(t, "GET", px.URL+"/", map[string]string{"Host": "localhost:" + itoa(port)})
	if resp.StatusCode != 401 {
		t.Fatalf("got %d, want the cloud's 401", resp.StatusCode)
	}
	up.srv.Close() // cloud unreachable
	resp, body := do(t, "GET", px.URL+"/", map[string]string{"Host": "localhost:" + itoa(port)})
	if resp.StatusCode != http.StatusBadGateway || strings.Contains(body, "uldk_secret") {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
}

func TestProxyCarriesWebSocketUpgrades(t *testing.T) {
	var gotAuth string
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		c, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.WriteMessage(websocket.TextMessage, []byte("hello over ws"))
	})
	px, port := proxyFor(t, up)
	d := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	h := http.Header{"Origin": {"http://localhost:" + itoa(port)}}
	conn, _, err := d.Dial("ws"+strings.TrimPrefix(px.URL, "http")+"/api/v1/containers/x/logs/stream", h)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, msg, err := conn.ReadMessage()
	if err != nil || string(msg) != "hello over ws" {
		t.Fatalf("%q %v", msg, err)
	}
	if gotAuth != "Bearer uldk_secret" {
		t.Fatalf("upstream Authorization %q", gotAuth)
	}
	// A cross-origin page cannot open the socket.
	if c, resp, err := d.Dial("ws"+strings.TrimPrefix(px.URL, "http")+"/x", http.Header{"Origin": {"https://evil.example"}}); err == nil {
		c.Close()
		t.Fatal("cross-origin WebSocket accepted")
	} else if resp == nil || resp.StatusCode != 403 {
		t.Fatalf("got %v %v", resp, err)
	}
}

func TestConnectListensOnLoopbackOnlyAndStops(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- Connect(ctx, ConnectOptions{
			Cred: Credentials{CloudURL: up.srv.URL + "/api", DeviceKey: "k"}, ClusterID: "cluster_abc",
			Ready: func(a string) { ready <- a },
		})
	}()
	addr := <-ready
	host, port, _ := net.SplitHostPort(addr)
	if host != "127.0.0.1" {
		t.Fatalf("listening on %q, want 127.0.0.1", host)
	}
	if resp, _ := do(t, "GET", "http://"+addr+"/", map[string]string{"Host": "localhost:" + port}); resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Connect did not stop")
	}
}

func TestConnectValidatesItsInputs(t *testing.T) {
	good := Credentials{CloudURL: "https://c.example/api", DeviceKey: "k"}
	for _, id := range []string{"", "a/b", "a b", "a?x", "a%2fb"} {
		if err := Connect(context.Background(), ConnectOptions{Cred: good, ClusterID: id}); err == nil {
			t.Errorf("cluster id %q accepted", id)
		}
	}
	if err := Connect(context.Background(), ConnectOptions{Cred: Credentials{CloudURL: "http://cloud.example/api", DeviceKey: "k"}, ClusterID: "c"}); err == nil {
		t.Error("device key allowed over plain http to a remote host")
	}
}
