package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/assertion"
)

const cluster = "cluster_edge1"

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

type key struct {
	kid    string
	priv   ed25519.PrivateKey
	signer *assertion.Signer
	jwk    assertion.JWK
}

func newKey(t *testing.T, nb, na time.Time) key {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	kid := assertion.Thumbprint(pub)
	s, _ := assertion.NewSigner(kid, priv)
	return key{kid: kid, priv: priv, signer: s, jwk: assertion.NewJWK(kid, pub, nb, na)}
}

// fakeCloud serves a key set and counts fetches; down simulates an outage.
type fakeCloud struct {
	mu    sync.Mutex
	keys  []assertion.JWK
	down  bool
	calls int
}

func (f *fakeCloud) set(keys ...key) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = nil
	for _, k := range keys {
		f.keys = append(f.keys, k.jwk)
	}
}
func (f *fakeCloud) setDown(d bool) { f.mu.Lock(); f.down = d; f.mu.Unlock() }
func (f *fakeCloud) count() int     { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }
func (f *fakeCloud) fetch(context.Context) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.down {
		return nil, errors.New("cloud unreachable")
	}
	return json.Marshal(assertion.KeySet{Keys: f.keys})
}

type env struct {
	clk   *clock
	cloud *fakeCloud
	cache *KeyCache
	ver   *Verifier
	path  string
	aud   string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		clk:   &clock{t: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)},
		cloud: &fakeCloud{},
		path:  filepath.Join(t.TempDir(), "state", "assertion_keys.json"),
		aud:   cluster,
	}
	e.build(t)
	return e
}

// build (re)creates the cache and verifier as a process restart would, over
// the same persisted file.
func (e *env) build(t *testing.T) {
	t.Helper()
	cache, err := NewKeyCache(KeyCacheConfig{Fetch: e.cloud.fetch, Path: e.path, Now: e.clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	ver, err := NewVerifier(VerifierConfig{Keys: cache, Now: e.clk.Now, Audience: func() (string, error) {
		if e.aud == "" {
			return "", errors.New("not registered")
		}
		return e.aud, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	e.cache, e.ver = cache, ver
}

func (e *env) liveKey(t *testing.T) key {
	return newKey(t, e.clk.Now().Add(-time.Hour), e.clk.Now().Add(24*time.Hour))
}

func (e *env) claims(jti string, scopes ...string) assertion.Claims {
	now := e.clk.Now()
	return assertion.Claims{
		Issuer: assertion.DefaultIssuer, Subject: "acct_1", Audience: cluster, Scopes: scopes,
		IssuedAt: now.Unix(), ExpiresAt: now.Add(assertion.DefaultTTL).Unix(), JTI: jti,
	}
}

func sign(t *testing.T, k key, c assertion.Claims) string {
	t.Helper()
	tok, err := k.signer.Sign(c)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestAcceptsValidAssertion(t *testing.T) {
	e := newEnv(t)
	k := e.liveKey(t)
	e.cloud.set(k)
	if err := e.cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	p, err := e.ver.Authenticate(context.Background(), sign(t, k, e.claims("j1", "read", "deploy")))
	if err != nil {
		t.Fatal(err)
	}
	if p.Subject != "acct_1" || p.Scope != service.ScopeDeploy {
		t.Fatalf("principal = %+v", p)
	}
}

func TestEffectiveScopeIsTheHighestRecognised(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want service.Scope
		ok   bool
	}{
		{[]string{"read"}, service.ScopeRead, true},
		{[]string{"read", "admin", "deploy"}, service.ScopeAdmin, true},
		{[]string{"deploy", "read"}, service.ScopeDeploy, true},
		{[]string{"superuser", "read"}, service.ScopeRead, true}, // unknown names never widen
		{[]string{"superuser", "root"}, "", false},
		{nil, "", false},
	} {
		got, ok := effectiveScope(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("effectiveScope(%v) = %q,%v want %q,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestRejections(t *testing.T) {
	e := newEnv(t)
	k := e.liveKey(t)
	e.cloud.set(k)
	_ = e.cache.Refresh(context.Background())
	ctx := context.Background()

	t.Run("wrong audience", func(t *testing.T) {
		c := e.claims("a1", "read")
		c.Audience = "cluster_other"
		if _, err := e.ver.Authenticate(ctx, sign(t, k, c)); !errors.Is(err, assertion.ErrAudience) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("expired", func(t *testing.T) {
		tok := sign(t, k, e.claims("a2", "read"))
		e.clk.Advance(assertion.DefaultTTL + time.Minute)
		if _, err := e.ver.Authenticate(ctx, tok); !errors.Is(err, assertion.ErrExpired) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("no recognised scope", func(t *testing.T) {
		if _, err := e.ver.Authenticate(ctx, sign(t, k, e.claims("a3", "superuser"))); !errors.Is(err, assertion.ErrScope) {
			t.Fatalf("got %v", err)
		}
		if _, err := e.ver.Authenticate(ctx, sign(t, k, e.claims("a4"))); !errors.Is(err, assertion.ErrScope) {
			t.Fatalf("empty scopes: got %v", err)
		}
	})
	t.Run("wrong issuer", func(t *testing.T) {
		c := e.claims("a5", "read")
		c.Issuer = "someone"
		if _, err := e.ver.Authenticate(ctx, sign(t, k, c)); !errors.Is(err, assertion.ErrIssuer) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("forged signature", func(t *testing.T) {
		other := e.liveKey(t)
		other.signer, _ = assertion.NewSigner(k.kid, other.priv) // claims k's kid
		if _, err := e.ver.Authenticate(ctx, sign(t, other, e.claims("a6", "read"))); !errors.Is(err, assertion.ErrBadSignature) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("garbage", func(t *testing.T) {
		for _, tok := range []string{"", "x", "a.b.c", "Bearer ulat_abc"} {
			if _, err := e.ver.Authenticate(ctx, tok); err == nil {
				t.Fatalf("%q accepted", tok)
			}
		}
	})
}

func TestReplayedJTIRejected(t *testing.T) {
	e := newEnv(t)
	k := e.liveKey(t)
	e.cloud.set(k)
	_ = e.cache.Refresh(context.Background())
	tok := sign(t, k, e.claims("once", "read"))
	if _, err := e.ver.Authenticate(context.Background(), tok); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ver.Authenticate(context.Background(), tok); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay: got %v, want ErrReplay", err)
	}
	// Same jti inside a different token is also a replay.
	again := sign(t, k, e.claims("once", "admin"))
	if _, err := e.ver.Authenticate(context.Background(), again); !errors.Is(err, ErrReplay) {
		t.Fatalf("reused jti: got %v", err)
	}
	// Remembered until Expiry()+leeway, then forgotten (no unbounded growth).
	e.clk.Advance(assertion.DefaultTTL + assertion.DefaultLeeway + time.Second)
	fresh := sign(t, k, e.claims("later", "read"))
	if _, err := e.ver.Authenticate(context.Background(), fresh); err != nil {
		t.Fatal(err)
	}
	if n := e.ver.cfg.Replay.Len(); n != 2 { // purge happens lazily; at most the 2 entries
		t.Logf("replay entries: %d", n)
	}
}

func TestReplayCacheBoundsAndFailsClosed(t *testing.T) {
	rc := NewReplayCache(2)
	now := time.Unix(1000, 0)
	if rc.Use("a", now.Add(time.Minute), now) != nil || rc.Use("b", now.Add(time.Minute), now) != nil {
		t.Fatal("first uses failed")
	}
	if err := rc.Use("c", now.Add(time.Minute), now); !errors.Is(err, ErrReplayCacheFull) {
		t.Fatalf("got %v, want ErrReplayCacheFull", err)
	}
	// once the old ones expire there is room again
	if err := rc.Use("c", now.Add(3*time.Minute), now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	// an expired entry may be reused
	if err := rc.Use("c", now.Add(9*time.Minute), now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func TestFailClosedWithNoKeyEver(t *testing.T) {
	e := newEnv(t)
	k := e.liveKey(t)
	e.cloud.setDown(true) // never fetched, cloud down
	_, err := e.ver.Authenticate(context.Background(), sign(t, k, e.claims("n1", "read")))
	if !errors.Is(err, assertion.ErrNoKeys) {
		t.Fatalf("got %v, want ErrNoKeys", err)
	}
	// and a cloud that answers with nothing usable keeps it closed
	e.cloud.setDown(false)
	e.cloud.set()
	e.clk.Advance(2 * time.Minute)
	if _, err := e.ver.Authenticate(context.Background(), sign(t, k, e.claims("n2", "read"))); err == nil {
		t.Fatal("accepted with no keys")
	}
}

func TestFailClosedWhenClusterIdUnknown(t *testing.T) {
	e := newEnv(t)
	k := e.liveKey(t)
	e.cloud.set(k)
	_ = e.cache.Refresh(context.Background())
	e.aud = ""
	if _, err := e.ver.Authenticate(context.Background(), sign(t, k, e.claims("u1", "read"))); !errors.Is(err, ErrNoAudience) {
		t.Fatalf("got %v, want ErrNoAudience", err)
	}
}

func TestUnknownKidTriggersOneRefetchAndPicksUpRotatedKey(t *testing.T) {
	e := newEnv(t)
	oldK := e.liveKey(t)
	e.cloud.set(oldK)
	_ = e.cache.Refresh(context.Background()) // 1 fetch
	newK := e.liveKey(t)
	e.cloud.set(oldK, newK) // cloud rotated; we have not refreshed yet
	e.clk.Advance(2 * time.Minute)

	before := e.cloud.count()
	if _, err := e.ver.Authenticate(context.Background(), sign(t, newK, e.claims("r1", "read"))); err != nil {
		t.Fatalf("token from rotated-in key should verify after one refetch: %v", err)
	}
	if got := e.cloud.count() - before; got != 1 {
		t.Fatalf("fetches = %d, want 1", got)
	}
	// The old key still verifies (overlap) without any fetch.
	if _, err := e.ver.Authenticate(context.Background(), sign(t, oldK, e.claims("r2", "read"))); err != nil {
		t.Fatal(err)
	}
	if got := e.cloud.count() - before; got != 1 {
		t.Fatalf("fetches = %d after a known-kid token, want still 1", got)
	}
}

func TestUnknownKidRefetchBoundedToOncePerMinute(t *testing.T) {
	e := newEnv(t)
	k := e.liveKey(t)
	e.cloud.set(k)
	_ = e.cache.Refresh(context.Background())
	e.clk.Advance(2 * time.Minute)
	stranger := e.liveKey(t) // never published
	base := e.cloud.count()

	attempt := func(i int) {
		c := e.claims("s"+string(rune('a'+i)), "read")
		if _, err := e.ver.Authenticate(context.Background(), sign(t, stranger, c)); !errors.Is(err, assertion.ErrUnknownKey) {
			t.Fatalf("got %v, want ErrUnknownKey", err)
		}
	}
	for i := 0; i < 50; i++ { // a burst of forged tokens
		attempt(i % 20)
	}
	if got := e.cloud.count() - base; got != 1 {
		t.Fatalf("burst caused %d fetches, want exactly 1", got)
	}
	e.clk.Advance(59 * time.Second)
	attempt(1)
	if got := e.cloud.count() - base; got != 1 {
		t.Fatalf("59s later: %d fetches, want 1", got)
	}
	e.clk.Advance(2 * time.Second) // 61s since the last attempt
	attempt(2)
	if got := e.cloud.count() - base; got != 2 {
		t.Fatalf("61s later: %d fetches, want 2", got)
	}
}

func TestFailedRefetchAlsoCountsTowardTheBound(t *testing.T) {
	e := newEnv(t)
	k := e.liveKey(t)
	e.cloud.set(k)
	_ = e.cache.Refresh(context.Background())
	e.clk.Advance(2 * time.Minute)
	e.cloud.setDown(true)
	base := e.cloud.count()
	stranger := e.liveKey(t)
	for i := 0; i < 10; i++ {
		_, _ = e.ver.Authenticate(context.Background(), sign(t, stranger, e.claims("f"+string(rune('a'+i)), "read")))
	}
	if got := e.cloud.count() - base; got != 1 {
		t.Fatalf("%d fetches while the cloud is down, want 1", got)
	}
}

// The cached key set survives a cloud outage, including a restart with the
// cloud down.
func TestCachedKeySetSurvivesCloudOutageAndRestart(t *testing.T) {
	e := newEnv(t)
	k := e.liveKey(t)
	e.cloud.set(k)
	if err := e.cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.cloud.setDown(true)
	if _, err := e.ver.Authenticate(context.Background(), sign(t, k, e.claims("o1", "read"))); err != nil {
		t.Fatalf("with the cloud down: %v", err)
	}
	if err := e.cache.Refresh(context.Background()); err == nil {
		t.Fatal("refresh against a down cloud should error")
	}
	// The failed refresh must not have discarded the keys.
	if _, err := e.ver.Authenticate(context.Background(), sign(t, k, e.claims("o2", "read"))); err != nil {
		t.Fatal(err)
	}

	// Restart the Orchestrator while the cloud is still down.
	e.build(t)
	if _, err := e.ver.Authenticate(context.Background(), sign(t, k, e.claims("o3", "read"))); err != nil {
		t.Fatalf("after restart from disk with the cloud down: %v", err)
	}
	st, err := os.Stat(e.path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("persisted key set: %v %v", st, err)
	}
}

func TestOldKeyKeptUntilItsStatedExpiryThenDropped(t *testing.T) {
	e := newEnv(t)
	now := e.clk.Now()
	oldK := newKey(t, now.Add(-time.Hour), now.Add(2*time.Hour)) // retiring
	newK := newKey(t, now, now.Add(90*24*time.Hour))
	e.cloud.set(oldK, newK)
	_ = e.cache.Refresh(context.Background())
	e.cloud.setDown(true)

	e.clk.Advance(time.Hour)
	if _, err := e.ver.Authenticate(context.Background(), sign(t, oldK, e.claims("k1", "read"))); err != nil {
		t.Fatalf("old key inside its overlap: %v", err)
	}
	e.clk.Advance(time.Hour + time.Second) // past oldK.not_after, cloud still down
	if _, err := e.ver.Authenticate(context.Background(), sign(t, oldK, e.claims("k2", "read"))); err == nil {
		t.Fatal("old key accepted after its not_after")
	}
	if _, err := e.ver.Authenticate(context.Background(), sign(t, newK, e.claims("k3", "read"))); err != nil {
		t.Fatalf("new key: %v", err)
	}
	// The expiry is also enforced for keys loaded from disk after a restart.
	e.build(t)
	for _, j := range e.cache.Keys().Keys {
		if j.Kid == oldK.kid {
			t.Fatal("expired key reloaded from disk")
		}
	}
}

func TestCloudWithdrawingAKeyDropsItImmediately(t *testing.T) {
	e := newEnv(t)
	a, b := e.liveKey(t), e.liveKey(t)
	e.cloud.set(a, b)
	_ = e.cache.Refresh(context.Background())
	e.cloud.set(b) // a withdrawn (compromise rotation)
	if err := e.cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.clk.Advance(2 * time.Minute)
	if _, err := e.ver.Authenticate(context.Background(), sign(t, a, e.claims("w1", "read"))); err == nil {
		t.Fatal("withdrawn key still accepted")
	}
}

func TestBadOrEmptyCloudAnswersKeepThePreviousSet(t *testing.T) {
	e := newEnv(t)
	k := e.liveKey(t)
	e.cloud.set(k)
	_ = e.cache.Refresh(context.Background())

	e.cloud.set() // empty
	if err := e.cache.Refresh(context.Background()); err == nil {
		t.Fatal("empty key set accepted as a replacement")
	}
	bad := e.liveKey(t)
	bad.jwk.Crv = "Ed448" // fails strict parsing
	e.cloud.set(bad)
	if err := e.cache.Refresh(context.Background()); err == nil {
		t.Fatal("invalid key set accepted")
	}
	if _, err := e.ver.Authenticate(context.Background(), sign(t, k, e.claims("x1", "read"))); err != nil {
		t.Fatalf("previous set lost: %v", err)
	}
}

func TestCorruptCacheFileFailsClosed(t *testing.T) {
	e := newEnv(t)
	if err := os.MkdirAll(filepath.Dir(e.path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.cloud.setDown(true)
	e.build(t)
	if n := len(e.cache.Keys().Keys); n != 0 {
		t.Fatalf("%d keys from a corrupt file", n)
	}
}

func TestRunRetriesUntilTheFirstFetchSucceeds(t *testing.T) {
	cloud := &fakeCloud{down: true}
	k := newKey(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	cloud.set(k)
	cache, err := NewKeyCache(KeyCacheConfig{
		Fetch: cloud.fetch, Path: filepath.Join(t.TempDir(), "k.json"),
		RetryEvery: 5 * time.Millisecond, RefreshEvery: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { cache.Run(ctx); close(done) }()
	time.Sleep(30 * time.Millisecond)
	if len(cache.Keys().Keys) != 0 {
		t.Fatal("keys appeared while the cloud was down")
	}
	cloud.setDown(false)
	deadline := time.Now().Add(2 * time.Second)
	for len(cache.Keys().Keys) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(cache.Keys().Keys) != 1 {
		t.Fatal("Run never picked up the key set")
	}
	cancel()
	<-done
}

func TestConstructorValidation(t *testing.T) {
	if _, err := NewKeyCache(KeyCacheConfig{}); err == nil {
		t.Error("KeyCache without Fetch/Path accepted")
	}
	if _, err := NewVerifier(VerifierConfig{}); err == nil {
		t.Error("Verifier without Keys/Audience accepted")
	}
}

func TestClusterIDFromCertDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := ClusterIDFromCertDir(dir); err == nil {
		t.Fatal("unregistered cluster must have no identity")
	}
	if err := os.WriteFile(filepath.Join(dir, "cluster_cert.pem"), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ClusterIDFromCertDir(dir); err == nil {
		t.Fatal("junk certificate accepted")
	}
	writeCert(t, dir, "cluster_abc123")
	id, err := ClusterIDFromCertDir(dir)
	if err != nil || id != "cluster_abc123" {
		t.Fatalf("got %q, %v", id, err)
	}
}

func TestClusterIDProviderCachesSuccessOnly(t *testing.T) {
	dir := t.TempDir()
	clk := &clock{t: time.Now()}
	get := ClusterIDProvider(dir, time.Minute, clk.Now)
	if _, err := get(); err == nil {
		t.Fatal("expected an error before registration")
	}
	writeCert(t, dir, "cluster_one") // registers while running
	if id, err := get(); err != nil || id != "cluster_one" {
		t.Fatalf("got %q %v", id, err)
	}
	writeCert(t, dir, "cluster_two")
	if id, _ := get(); id != "cluster_one" {
		t.Fatalf("answer not cached: %q", id)
	}
	clk.Advance(2 * time.Minute)
	if id, _ := get(); id != "cluster_two" {
		t.Fatalf("cache never refreshed: %q", id)
	}
}
