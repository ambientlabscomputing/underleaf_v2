package remote

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/assertion"
)

// ErrNoAudience is returned when this cluster's id is unknown (for example the
// cluster is not registered). Without an audience nothing can be verified, so
// every assertion is rejected.
var ErrNoAudience = errors.New("remote: cluster id unknown; cannot verify assertions")

// Principal is who an accepted assertion acts as.
type Principal struct {
	// Subject is the account (or device) the cloud authenticated.
	Subject string
	// JTI identifies the assertion (for logs).
	JTI string
	// Scope is the effective scope: the highest of the assertion's scopes, on
	// the Orchestrator's hierarchical read < deploy < admin scale. Routes are
	// checked against it with Scope.Allows, using the same route table as the
	// local API tokens.
	Scope service.Scope
	// Expires is when the assertion stops being valid.
	Expires time.Time
}

// VerifierConfig configures a Verifier.
type VerifierConfig struct {
	Keys *KeyCache
	// Audience returns this cluster's id (assertions must carry it as `aud`).
	// An error or empty value rejects everything.
	Audience func() (string, error)
	Replay   *ReplayCache
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Leeway overrides assertion.DefaultLeeway (0 = default).
	Leeway time.Duration
}

// Verifier authenticates assertions for the remote listener.
type Verifier struct {
	cfg VerifierConfig
}

// NewVerifier returns a Verifier. Keys and Audience are required.
func NewVerifier(cfg VerifierConfig) (*Verifier, error) {
	if cfg.Keys == nil || cfg.Audience == nil {
		return nil, errors.New("remote: VerifierConfig needs Keys and Audience")
	}
	if cfg.Replay == nil {
		cfg.Replay = NewReplayCache(0)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Leeway == 0 {
		cfg.Leeway = assertion.DefaultLeeway
	}
	return &Verifier{cfg: cfg}, nil
}

// Authenticate verifies token and, only if every check passes, records its jti
// and returns the principal. Checks: signature by a cached, unexpired key;
// algorithm and type; issuer; audience = this cluster; expiry and lifetime; at
// least one known scope; and the jti has not been seen.
//
// An unknown key id (or an empty cache) triggers one bounded refetch of the key
// set, then the token is verified again. Every error means "reject".
func (v *Verifier) Authenticate(ctx context.Context, token string) (*Principal, error) {
	aud, err := v.cfg.Audience()
	if err != nil || aud == "" {
		return nil, ErrNoAudience
	}
	opts := assertion.VerifyOptions{
		Audience: aud,
		Issuer:   assertion.DefaultIssuer,
		Now:      v.cfg.Now,
		Leeway:   v.cfg.Leeway,
	}
	claims, err := assertion.Verify(token, v.cfg.Keys.Keys(), opts)
	if errors.Is(err, assertion.ErrUnknownKey) || errors.Is(err, assertion.ErrNoKeys) {
		if fetched, _ := v.cfg.Keys.RefreshOnDemand(ctx); fetched {
			claims, err = assertion.Verify(token, v.cfg.Keys.Keys(), opts)
		}
	}
	if err != nil {
		return nil, err
	}

	scope, ok := effectiveScope(claims.Scopes)
	if !ok {
		return nil, fmt.Errorf("%w: no recognised scope", assertion.ErrScope)
	}
	now := v.cfg.Now()
	if err := v.cfg.Replay.Use(claims.JTI, claims.Expiry().Add(v.cfg.Leeway), now); err != nil {
		return nil, err
	}
	return &Principal{Subject: claims.Subject, JTI: claims.JTI, Scope: scope, Expires: claims.Expiry()}, nil
}

// effectiveScope maps assertion scopes onto the Orchestrator's hierarchical
// scopes: the highest recognised one. Unknown names are ignored (never widen).
func effectiveScope(scopes []string) (service.Scope, bool) {
	var best service.Scope
	found := false
	for _, s := range scopes {
		sc, err := service.ParseScope(s)
		if err != nil {
			continue
		}
		if !found || sc.Allows(best) && !best.Allows(sc) {
			best, found = sc, true
		}
	}
	return best, found
}
