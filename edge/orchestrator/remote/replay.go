package remote

import (
	"errors"
	"sync"
	"time"
)

// ErrReplay is returned for a jti that was already accepted.
var ErrReplay = errors.New("remote: assertion already used (replay)")

// ErrReplayCacheFull is returned when the replay cache cannot take another
// entry. Failing closed here is deliberate: dropping entries would reopen
// replays.
var ErrReplayCacheFull = errors.New("remote: replay cache full")

// ReplayCache remembers accepted jti values until their assertion can no longer
// verify, so presenting the same assertion twice fails the second time. Only
// assertions that already passed full verification are ever inserted, so
// forged tokens cannot fill it.
//
// It is in memory: a restart forgets it, leaving a replay window of at most one
// assertion lifetime (about 60 s) right after the Orchestrator restarts.
type ReplayCache struct {
	mu    sync.Mutex
	seen  map[string]time.Time // jti -> forget-after
	limit int
}

// DefaultReplayLimit is the maximum number of live jtis remembered.
const DefaultReplayLimit = 100_000

// NewReplayCache creates a cache holding at most limit entries (0 = default).
func NewReplayCache(limit int) *ReplayCache {
	if limit <= 0 {
		limit = DefaultReplayLimit
	}
	return &ReplayCache{seen: map[string]time.Time{}, limit: limit}
}

// Use records jti as used until forgetAfter (the assertion's expiry plus the
// verifier's leeway). It returns ErrReplay if jti is still remembered.
func (rc *ReplayCache) Use(jti string, forgetAfter, now time.Time) error {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if until, ok := rc.seen[jti]; ok && now.Before(until) {
		return ErrReplay
	}
	if len(rc.seen) >= rc.limit {
		for k, until := range rc.seen {
			if !now.Before(until) {
				delete(rc.seen, k)
			}
		}
		if len(rc.seen) >= rc.limit {
			return ErrReplayCacheFull
		}
	}
	rc.seen[jti] = forgetAfter
	return nil
}

// Len is the number of remembered jtis (including ones not yet purged).
func (rc *ReplayCache) Len() int {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return len(rc.seen)
}
