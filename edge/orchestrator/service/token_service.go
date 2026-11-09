package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
)

// API tokens (RFD 5, step 6).
//
// A token is a bearer credential: "ulat_<id>_<secret>" where id is 12 hex
// characters (public, shown in listings, used to revoke) and secret is 64 hex
// characters (256 random bits). Only SHA-256(secret) is stored, so a read of
// the database (or a backup, or `orcli sql`) yields nothing usable; the secret
// is returned once, at creation. The secret is full-entropy, so a fast hash is
// appropriate (there is nothing to brute-force).
//
// Tokens are minted over the owner-only unix socket (see grpc_private), and
// every token expires: there are no eternal tokens.

// Scope is what a token may do.
type Scope string

const (
	// ScopeRead permits read-only API calls (GET-style).
	ScopeRead Scope = "read"
	// ScopeDeploy additionally permits deploying and running workloads. Note:
	// this runs Docker on the host and is root-equivalent (RFD "Authorization").
	ScopeDeploy Scope = "deploy"
	// ScopeAdmin permits everything, including any route that has not been
	// given a narrower scope explicitly.
	ScopeAdmin Scope = "admin"
)

// ParseScope validates a scope name.
func ParseScope(s string) (Scope, error) {
	switch Scope(s) {
	case ScopeRead, ScopeDeploy, ScopeAdmin:
		return Scope(s), nil
	}
	return "", fmt.Errorf("invalid scope %q: want read, deploy or admin", s)
}

func (s Scope) rank() int {
	switch s {
	case ScopeRead:
		return 1
	case ScopeDeploy:
		return 2
	case ScopeAdmin:
		return 3
	}
	return 0 // unknown scopes allow nothing
}

// Allows reports whether holding s satisfies a route that requires 'required'.
// Scopes are hierarchical: admin includes deploy includes read.
func (s Scope) Allows(required Scope) bool {
	return s.rank() > 0 && required.rank() > 0 && s.rank() >= required.rank()
}

const (
	tokenPrefix    = "ulat_"
	tokenIDLen     = 12 // hex chars
	tokenSecretLen = 64 // hex chars
	tokenNameMax   = 64

	// MinTokenTTL and MaxTokenTTL bound the lifetime of a token.
	MinTokenTTL = time.Minute
	MaxTokenTTL = 365 * 24 * time.Hour
	// DefaultTokenTTL is used by the CLI when --ttl is not given.
	DefaultTokenTTL = 30 * 24 * time.Hour

	lastUsedGranularity = time.Minute
)

// ErrInvalidToken is returned (wrapped, with the internal reason) for every
// way a presented token can fail: malformed, unknown, wrong secret, expired,
// revoked. Callers must not reveal the reason to the client.
var ErrInvalidToken = errors.New("invalid token")

// TokenPrincipal is an authenticated token holder.
type TokenPrincipal struct {
	TokenID string
	Scope   Scope
}

// CreatedToken is the one-time result of minting a token.
type CreatedToken struct {
	Secret string // the full bearer token; shown once, never stored
	Info   *repository.APIToken
}

// TokenService mints, verifies, lists and revokes API tokens. It holds no
// state of its own, so any number of instances over one repository agree.
type TokenService struct {
	repo *repository.APITokenRepository
	now  func() time.Time
	rand func([]byte) error
}

func NewTokenService(repo *repository.APITokenRepository) *TokenService {
	return &TokenService{
		repo: repo,
		now:  time.Now,
		rand: func(b []byte) error { _, err := rand.Read(b); return err },
	}
}

// Tokens returns a token service over the orchestrator's repository.
func (s *AppService) Tokens() *TokenService {
	return NewTokenService(s.Repository.APITokens())
}

// WithClock returns a copy using now for time (tests).
func (s *TokenService) WithClock(now func() time.Time) *TokenService {
	c := *s
	c.now = now
	return &c
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func (s *TokenService) randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if err := s.rand(b); err != nil {
		return "", fmt.Errorf("token: random: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Create mints a token with the given scope and lifetime.
func (s *TokenService) Create(name string, scope Scope, ttl time.Duration) (*CreatedToken, error) {
	if _, err := ParseScope(string(scope)); err != nil {
		return nil, err
	}
	if ttl < MinTokenTTL || ttl > MaxTokenTTL {
		return nil, fmt.Errorf("ttl %s out of range: want between %s and %s (tokens always expire)", ttl, MinTokenTTL, MaxTokenTTL)
	}
	name = strings.TrimSpace(name)
	if len(name) > tokenNameMax {
		return nil, fmt.Errorf("name longer than %d characters", tokenNameMax)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return nil, errors.New("name contains control characters")
		}
	}

	id, err := s.randomHex(tokenIDLen / 2)
	if err != nil {
		return nil, err
	}
	secret, err := s.randomHex(tokenSecretLen / 2)
	if err != nil {
		return nil, err
	}
	now := s.now()
	rec := &repository.APIToken{
		ID:        id,
		Name:      name,
		Scope:     string(scope),
		TokenHash: hashSecret(secret),
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	if err := s.repo.Create(rec); err != nil {
		return nil, fmt.Errorf("token: store: %w", err)
	}
	// Re-read so the returned record matches storage (second precision).
	stored, err := s.repo.Get(id)
	if err != nil {
		return nil, err
	}
	return &CreatedToken{Secret: tokenPrefix + id + "_" + secret, Info: stored}, nil
}

func isLowerHex(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// parseToken splits a presented token into id and secret, strictly.
func parseToken(presented string) (id, secret string, ok bool) {
	rest, found := strings.CutPrefix(presented, tokenPrefix)
	if !found || len(rest) != tokenIDLen+1+tokenSecretLen || rest[tokenIDLen] != '_' {
		return "", "", false
	}
	id, secret = rest[:tokenIDLen], rest[tokenIDLen+1:]
	if !isLowerHex(id) || !isLowerHex(secret) {
		return "", "", false
	}
	return id, secret, true
}

// Authenticate verifies a presented bearer token. Every failure wraps
// ErrInvalidToken.
func (s *TokenService) Authenticate(_ context.Context, presented string) (*TokenPrincipal, error) {
	id, secret, ok := parseToken(presented)
	if !ok {
		return nil, fmt.Errorf("%w: malformed", ErrInvalidToken)
	}
	rec, err := s.repo.Get(id)
	got := hashSecret(secret)
	if err != nil {
		if errors.Is(err, repository.ErrAPITokenNotFound) {
			return nil, fmt.Errorf("%w: unknown id", ErrInvalidToken)
		}
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(rec.TokenHash)) != 1 {
		return nil, fmt.Errorf("%w: wrong secret for %s", ErrInvalidToken, id)
	}
	now := s.now()
	if rec.RevokedAt != nil {
		return nil, fmt.Errorf("%w: %s revoked", ErrInvalidToken, id)
	}
	if !now.Before(rec.ExpiresAt) {
		return nil, fmt.Errorf("%w: %s expired", ErrInvalidToken, id)
	}
	scope, err := ParseScope(rec.Scope)
	if err != nil {
		return nil, fmt.Errorf("%w: %s has unknown scope", ErrInvalidToken, id)
	}
	_ = s.repo.TouchLastUsed(id, now, now.Add(-lastUsedGranularity)) // best effort
	return &TokenPrincipal{TokenID: id, Scope: scope}, nil
}

// List returns all tokens (never secrets), newest first.
func (s *TokenService) List() ([]*repository.APIToken, error) {
	return s.repo.List()
}

// Revoke invalidates a token immediately. Unknown ids return
// repository.ErrAPITokenNotFound.
func (s *TokenService) Revoke(id string) error {
	return s.repo.Revoke(strings.TrimSpace(id), s.now())
}

// TokenStatus describes a token's state at a point in time.
func TokenStatus(t *repository.APIToken, now time.Time) string {
	switch {
	case t.RevokedAt != nil:
		return "revoked"
	case !now.Before(t.ExpiresAt):
		return "expired"
	}
	return "active"
}
