// Package remote verifies the cloud-signed assertions that gate the
// Orchestrator's remote listener (RFD 5, "Remote access").
//
// The pieces: KeyCache fetches, persists and refreshes the cloud's assertion
// key set; ReplayCache remembers jti values; Verifier combines them with
// shared/assertion's stateless Verify.
package remote

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/assertion"
)

const (
	// DefaultMinRefetch bounds how often an unknown key id (or an empty cache)
	// may trigger a fetch: at most one per minute, so forged assertions cannot
	// turn the Orchestrator into a fetch amplifier.
	DefaultMinRefetch = time.Minute
	// DefaultRefreshEvery is the periodic refresh. It must stay well inside the
	// cloud's rotation overlap (168 h by default) so a new key is known before
	// the old one expires.
	DefaultRefreshEvery = time.Hour
	// DefaultRetryEvery is how soon Run retries after a failed fetch (including
	// "not registered yet").
	DefaultRetryEvery = DefaultMinRefetch

	fetchTimeout = 10 * time.Second
)

// FetchFunc returns the raw key set document from the cloud.
type FetchFunc func(ctx context.Context) ([]byte, error)

// KeyCacheConfig configures a KeyCache. Fetch and Path are required.
type KeyCacheConfig struct {
	// Fetch retrieves the key set. It is called with a timeout.
	Fetch FetchFunc
	// Path is where the last good key set is persisted (and reloaded from at
	// start, so verification works with the cloud unreachable).
	Path string
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// MinRefetch bounds on-demand refetches; zero means DefaultMinRefetch.
	MinRefetch time.Duration
	// RefreshEvery is the periodic refresh used by Run; zero means
	// DefaultRefreshEvery.
	RefreshEvery time.Duration
	// RetryEvery is how soon Run retries after a failed fetch; zero means
	// DefaultRetryEvery.
	RetryEvery time.Duration
	// Logf receives diagnostics; nil discards them.
	Logf func(format string, args ...any)
}

// KeyCache holds the last good assertion key set.
//
// Rules (RFD 5): fail closed (no key ever fetched means every assertion is
// rejected); keep a key until its stated not_after (Keys prunes by the clock,
// so an expired key is gone even if the cloud never answers again); the
// published set is authoritative on a successful fetch, so a key the cloud
// withdrew early (compromise) is dropped immediately; a fetch that fails or
// yields an unusable document leaves the previous set in place.
type KeyCache struct {
	cfg KeyCacheConfig

	mu   sync.RWMutex
	keys *assertion.KeySet

	fetchMu     sync.Mutex // serializes fetches; TryLock'd by on-demand refetches
	lastAttempt time.Time  // guarded by attemptMu
	attemptMu   sync.Mutex
}

// NewKeyCache creates a cache and loads the persisted key set, if any. A
// missing or corrupt file is not an error: the cache starts empty and fails
// closed until a fetch succeeds.
func NewKeyCache(cfg KeyCacheConfig) (*KeyCache, error) {
	if cfg.Fetch == nil || cfg.Path == "" {
		return nil, errors.New("remote: KeyCacheConfig needs Fetch and Path")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MinRefetch == 0 {
		cfg.MinRefetch = DefaultMinRefetch
	}
	if cfg.RefreshEvery == 0 {
		cfg.RefreshEvery = DefaultRefreshEvery
	}
	if cfg.RetryEvery == 0 {
		cfg.RetryEvery = DefaultRetryEvery
	}
	kc := &KeyCache{cfg: cfg, keys: &assertion.KeySet{}}
	kc.load()
	return kc, nil
}

func (kc *KeyCache) logf(format string, args ...any) {
	if kc.cfg.Logf != nil {
		kc.cfg.Logf(format, args...)
	}
}

func (kc *KeyCache) load() {
	raw, err := os.ReadFile(kc.cfg.Path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			kc.logf("assertion key cache: cannot read %s: %v", kc.cfg.Path, err)
		}
		return
	}
	ks, err := assertion.ParseKeySet(raw)
	if err != nil {
		kc.logf("assertion key cache: ignoring corrupt %s: %v", kc.cfg.Path, err)
		return
	}
	kc.keys = ks.Prune(kc.cfg.Now())
}

// Keys returns the usable key set: the cached keys that have not passed their
// not_after. The result is a snapshot and must not be modified.
func (kc *KeyCache) Keys() *assertion.KeySet {
	kc.mu.RLock()
	ks := kc.keys
	kc.mu.RUnlock()
	return ks.Prune(kc.cfg.Now())
}

// Refresh fetches the key set now, validates it and, if usable, replaces the
// cache and persists it. It does not apply the on-demand rate limit (Run and
// start-up call it directly).
func (kc *KeyCache) Refresh(ctx context.Context) error {
	kc.fetchMu.Lock()
	defer kc.fetchMu.Unlock()
	return kc.refreshLocked(ctx)
}

func (kc *KeyCache) refreshLocked(ctx context.Context) error {
	kc.attemptMu.Lock()
	kc.lastAttempt = kc.cfg.Now()
	kc.attemptMu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	raw, err := kc.cfg.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("fetch assertion keys: %w", err)
	}
	ks, err := assertion.ParseKeySet(raw)
	if err != nil {
		return fmt.Errorf("assertion keys from cloud rejected: %w", err)
	}
	ks = ks.Prune(kc.cfg.Now())
	if len(ks.Keys) == 0 {
		// Refusing to replace a working set with nothing: an empty or
		// all-expired document is a cloud fault, not a revocation of every key.
		return errors.New("assertion keys from cloud contain no unexpired key; keeping the previous set")
	}
	if err := writeAtomic(kc.cfg.Path, ks); err != nil {
		// Still use the fetched keys; only the persistence is degraded.
		kc.logf("assertion key cache: cannot persist %s: %v", kc.cfg.Path, err)
	}
	kc.mu.Lock()
	kc.keys = ks
	kc.mu.Unlock()
	return nil
}

// RefreshOnDemand is what verification calls after meeting an unknown key id
// or finding the cache empty. It fetches at most once per MinRefetch (counted
// from the last attempt of any kind, successful or not) and never blocks
// behind a fetch already in flight. It reports whether it fetched.
func (kc *KeyCache) RefreshOnDemand(ctx context.Context) (fetched bool, err error) {
	if !kc.fetchMu.TryLock() {
		return false, nil
	}
	defer kc.fetchMu.Unlock()

	kc.attemptMu.Lock()
	last := kc.lastAttempt
	kc.attemptMu.Unlock()
	if !last.IsZero() && kc.cfg.Now().Sub(last) < kc.cfg.MinRefetch {
		return false, nil
	}
	err = kc.refreshLocked(ctx)
	if err != nil {
		kc.logf("assertion key cache: refetch after unknown key id failed: %v", err)
	}
	return true, err
}

// Run keeps the cache fresh until ctx is done: it fetches immediately, then
// every RefreshEvery, retrying every minute while fetches fail (for example
// before the cluster is registered, when there is no pinned CA to fetch over).
func (kc *KeyCache) Run(ctx context.Context) {
	for {
		wait := kc.cfg.RefreshEvery
		if err := kc.Refresh(ctx); err != nil {
			kc.logf("assertion key cache: %v", err)
			wait = kc.cfg.RetryEvery
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func writeAtomic(path string, ks *assertion.KeySet) error {
	raw, err := ks.Marshal()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
