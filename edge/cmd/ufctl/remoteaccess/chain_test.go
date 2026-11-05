package remoteaccess_test

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/lib/conn_client"
	"github.com/ambientlabscomputing/underleaf_v2/edge/cmd/ufctl/remoteaccess"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/rest"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/remote"
	"github.com/ambientlabscomputing/underleaf_v2/shared/assertion"
)

const clusterID = "cluster_chain1"

// fakeCloud stands in for the missing cloud half: it authenticates a device key,
// checks ownership, mints an assertion per request, and forwards the request
// down a yamux stream to the agent (preamble first), exactly as the contract in
// doc.go describes. NOT the real cloud: that part does not exist yet.
type fakeCloud struct {
	t       *testing.T
	signer  *assertion.Signer
	tunnel  *yamux.Session
	devices map[string]string // device key -> account
	owners  map[string]string // cluster -> account
	scopes  []string
	mu      sync.Mutex
}

func (f *fakeCloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const prefix = "/api/v2/cloud/clusters/"
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	account, ok := f.devices[key]
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rest, found := strings.CutPrefix(r.URL.Path, prefix)
	cluster, tail, _ := strings.Cut(rest, "/remote")
	if !found || f.owners[cluster] != account {
		http.Error(w, "not found", http.StatusNotFound) // same answer for foreign and missing clusters
		return
	}
	now := time.Now()
	tok, err := f.signer.Sign(assertion.Claims{
		Issuer: assertion.DefaultIssuer, Subject: account, Audience: cluster, Scopes: f.scopes,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(assertion.DefaultTTL).Unix(), JTI: fmt.Sprintf("j%d", now.UnixNano()),
	})
	if err != nil {
		http.Error(w, "sign", 500)
		return
	}
	f.mu.Lock()
	s, err := f.tunnel.Open()
	f.mu.Unlock()
	if err != nil {
		http.Error(w, "tunnel down", http.StatusBadGateway)
		return
	}
	defer s.Close()
	fmt.Fprint(s, assertion.StreamPreamble)
	out := r.Clone(r.Context())
	out.URL.Path, out.RequestURI = tail, ""
	out.Header.Del(assertion.HeaderName)
	out.Header.Set(assertion.HeaderName, tok)
	out.Header.Del("Authorization")
	out.Header.Set("Connection", "close")
	out.Host = "cluster.internal"
	if err := out.Write(s); err != nil {
		http.Error(w, "write", 502)
		return
	}
	resp, err := http.ReadResponse(bufio.NewReader(s), r)
	if err != nil {
		http.Error(w, "no response", 502)
		return
	}
	defer resp.Body.Close()
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func tcpPair(t *testing.T) (cloud, agent *yamux.Session) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	ch := make(chan *yamux.Session, 1)
	go func() { c, _ := ln.Accept(); s, _ := yamux.Server(c, nil); ch <- s }()
	c, _ := net.Dial("tcp", ln.Addr().String())
	agent, _ = yamux.Client(c, nil)
	cloud = <-ch
	t.Cleanup(func() { cloud.Close(); agent.Close() })
	return
}

// ufctl login -> ufctl connect -> cloud -> tunnel -> agent -> remote listener.
func TestOwnerReachesTheOrchestratorThroughTheWholeChain(t *testing.T) {
	t.Setenv(remoteaccess.EnvHome, filepath.Join(t.TempDir(), "home"))

	// cluster edge: key cache fed by the "cloud", verifier, remote listener
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	kid := assertion.Thumbprint(pub)
	signer, _ := assertion.NewSigner(kid, priv)
	jwks, _ := json.Marshal(assertion.KeySet{Keys: []assertion.JWK{assertion.NewJWK(kid, pub, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))}})
	cache, _ := remote.NewKeyCache(remote.KeyCacheConfig{
		Path:  filepath.Join(t.TempDir(), "keys.json"),
		Fetch: func(context.Context) ([]byte, error) { return jwks, nil },
	})
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	ver, _ := remote.NewVerifier(remote.VerifierConfig{Keys: cache, Audience: func() (string, error) { return clusterID, nil }})
	orch := &rest.OrchestratorRESTServer{Remote: ver}
	orchSrv := httptest.NewServer(orch.RemoteHandler("/api/v1/", ""))
	defer orchSrv.Close()

	// agent <-> cloud tunnel
	cloudSess, agentSess := tcpPair(t)
	go conn_client.ServeRemoteStreams(context.Background(), agentSess, func() string { return strings.TrimPrefix(orchSrv.URL, "http://") })

	// the cloud
	cloud := &fakeCloud{t: t, signer: signer, tunnel: cloudSess, scopes: []string{"read"},
		devices: map[string]string{"uldk_owner": "acct_owner", "uldk_other": "acct_other"},
		owners:  map[string]string{clusterID: "acct_owner"}}
	cloudSrv := httptest.NewServer(cloud)
	defer cloudSrv.Close()

	// ufctl: store the device key as login would, then connect
	if err := remoteaccess.Save(remoteaccess.Credentials{CloudURL: cloudSrv.URL + "/api/v2/cloud", DeviceKey: "uldk_owner"}); err != nil {
		t.Fatal(err)
	}
	cred, err := remoteaccess.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan string, 1)
	go func() {
		_ = remoteaccess.Connect(ctx, remoteaccess.ConnectOptions{Cred: *cred, ClusterID: clusterID, Ready: func(a string) { ready <- a }})
	}()
	addr := <-ready
	_, port, _ := net.SplitHostPort(addr)

	get := func(method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, "http://"+addr+path, strings.NewReader(body))
		req.Host = "localhost:" + port
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	if code, body := get("GET", "/api/v1/health", ""); code != 200 || !strings.Contains(body, "OK") {
		t.Fatalf("health through the chain: %d %s", code, body)
	}
	// scope travels end to end: the owner's assertion is read-only here
	if code, _ := get("POST", "/api/v1/deployments", `{"source":"gh:a/b"}`); code != 403 {
		t.Fatalf("read-scoped owner deploying: %d, want 403", code)
	}
	cloud.scopes = []string{"read", "deploy"}
	if code, _ := get("POST", "/api/v1/deployments", `{}`); code != 400 { // passed authz; handler rejects the body
		t.Fatalf("deploy-scoped owner: %d, want 400 from the handler", code)
	}

	// Someone else's device key gets nothing, and nothing reaches the cluster.
	cred.DeviceKey = "uldk_other"
	h, _ := remoteaccess.NewProxyHandler(*cred, clusterID, 1, nil, io.Discard)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	req.Host = "localhost:1"
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-owner: %d, want 404", rec.Code)
	}
	// And a device key the cloud does not know.
	cred.DeviceKey = "uldk_revoked"
	h, _ = remoteaccess.NewProxyHandler(*cred, clusterID, 1, nil, io.Discard)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown device key: %d, want 401", rec.Code)
	}
}
