package assertion

import (
	"errors"
	"time"
)

const (
	// Algorithm is the only JWS algorithm accepted for assertions.
	Algorithm = "EdDSA"
	// TokenType is the required JOSE "typ" header value.
	TokenType = "underleaf-assertion+jwt"
	// DefaultIssuer is the "iss" the cloud puts in assertions.
	DefaultIssuer = "underleaf-cloud"

	// DefaultTTL is the lifetime the cloud gives an assertion.
	DefaultTTL = 60 * time.Second
	// DefaultMaxLifetime is the longest exp-iat a verifier accepts by default.
	DefaultMaxLifetime = 5 * time.Minute
	// DefaultLeeway is the clock skew a verifier tolerates by default.
	DefaultLeeway = 5 * time.Second

	// maxTokenBytes bounds what Verify will parse.
	maxTokenBytes = 8 * 1024
)

// Scopes an assertion can carry. They mirror the API token scopes of RFD 5
// ("Authorization"). Verify matches scopes exactly; any implication between
// them (admin includes deploy includes read) is the caller's policy.
const (
	ScopeRead   = "read"
	ScopeDeploy = "deploy"
	ScopeAdmin  = "admin"
)

// ValidScope reports whether s is a scope the cloud may put in an assertion.
func ValidScope(s string) bool {
	switch s {
	case ScopeRead, ScopeDeploy, ScopeAdmin:
		return true
	}
	return false
}

// Header is the JOSE header of an assertion.
type Header struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
	Kid string `json:"kid"`
}

// Claims is the payload of an assertion.
type Claims struct {
	// Issuer is the signing service (DefaultIssuer).
	Issuer string `json:"iss"`
	// Subject is the principal the request acts for: the account (or device)
	// id the cloud authenticated.
	Subject string `json:"sub"`
	// Audience is the id of the one cluster this assertion is valid for.
	Audience string `json:"aud"`
	// Scopes are the permissions granted (ScopeRead, ScopeDeploy, ScopeAdmin).
	Scopes []string `json:"scopes"`
	// IssuedAt and ExpiresAt are unix seconds.
	IssuedAt  int64 `json:"iat"`
	ExpiresAt int64 `json:"exp"`
	// JTI is a unique id; the Orchestrator rejects a jti it has already seen
	// until exp (replay protection, step 7b).
	JTI string `json:"jti"`
}

// HasScope reports whether the claims carry scope s exactly.
func (c *Claims) HasScope(s string) bool {
	for _, have := range c.Scopes {
		if have == s {
			return true
		}
	}
	return false
}

// Expiry returns ExpiresAt as a time, for sizing a jti replay cache entry.
func (c *Claims) Expiry() time.Time { return time.Unix(c.ExpiresAt, 0) }

// Verification failures. Verify wraps these, so callers branch with errors.Is.
// All of them mean "reject the request"; the distinction exists for logging
// and for the caller's key-refresh policy (only ErrUnknownKey should trigger a
// key set refetch, and 7b rate limits that).
var (
	ErrMalformed    = errors.New("assertion: malformed token")
	ErrAlgorithm    = errors.New("assertion: unacceptable algorithm or token type")
	ErrNoKeys       = errors.New("assertion: no verification keys available")
	ErrUnknownKey   = errors.New("assertion: unknown key id")
	ErrKeyExpired   = errors.New("assertion: signing key outside its validity window")
	ErrBadSignature = errors.New("assertion: signature invalid")
	ErrIssuer       = errors.New("assertion: wrong issuer")
	ErrExpired      = errors.New("assertion: expired")
	ErrNotYetValid  = errors.New("assertion: issued in the future")
	ErrLifetime     = errors.New("assertion: lifetime exceeds maximum")
	ErrAudience     = errors.New("assertion: wrong audience")
	ErrScope        = errors.New("assertion: missing required scope")
)
