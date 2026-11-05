package assertion

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// VerifyOptions configures Verify.
type VerifyOptions struct {
	// Audience is this cluster's id. Required: an empty value is an error, not
	// a wildcard.
	Audience string
	// Issuer, if set, must equal the token's iss.
	Issuer string
	// RequiredScopes must all be present in the token. Empty means no scope
	// requirement (the caller then must check HasScope itself).
	RequiredScopes []string
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
	// Leeway is tolerated clock skew. Zero means DefaultLeeway; negative
	// disables leeway.
	Leeway time.Duration
	// MaxLifetime caps exp-iat. Zero means DefaultMaxLifetime.
	MaxLifetime time.Duration
}

// Verify checks token against the key set and returns its claims. Checks, in
// order: size and shape; header algorithm and type (never taken from the
// token to select a method); key lookup by kid including the key's validity
// window; signature; then claims: issuer, exp, iat, lifetime, jti and subject
// present, audience, scopes. Any failure returns an error wrapping one of the
// Err* values and no claims.
//
// Verify is stateless. It does not detect replay: callers record Claims.JTI
// until Claims.ExpiresAt.
func Verify(token string, ks *KeySet, opts VerifyOptions) (*Claims, error) {
	if opts.Audience == "" {
		return nil, fmt.Errorf("assertion: VerifyOptions.Audience is required")
	}
	now := time.Now()
	if opts.Now != nil {
		now = opts.Now()
	}
	leeway := opts.Leeway
	switch {
	case leeway == 0:
		leeway = DefaultLeeway
	case leeway < 0:
		leeway = 0
	}
	maxLife := opts.MaxLifetime
	if maxLife == 0 {
		maxLife = DefaultMaxLifetime
	}

	if len(token) == 0 || len(token) > maxTokenBytes {
		return nil, fmt.Errorf("%w: bad length", ErrMalformed)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: want 3 segments, got %d", ErrMalformed, len(parts))
	}
	hdrRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: header encoding", ErrMalformed)
	}
	var hdr struct {
		Header
		Crit json.RawMessage `json:"crit"`
	}
	if err := json.Unmarshal(hdrRaw, &hdr); err != nil {
		return nil, fmt.Errorf("%w: header json", ErrMalformed)
	}
	// Fixed algorithm and type: reject everything else, including "none",
	// HS256/384/512 and RS256, before any key is consulted.
	if hdr.Alg != Algorithm {
		return nil, fmt.Errorf("%w: alg %q", ErrAlgorithm, hdr.Alg)
	}
	if hdr.Typ != TokenType {
		return nil, fmt.Errorf("%w: typ %q", ErrAlgorithm, hdr.Typ)
	}
	if len(hdr.Crit) > 0 {
		return nil, fmt.Errorf("%w: unsupported crit header", ErrAlgorithm)
	}
	if hdr.Kid == "" {
		return nil, fmt.Errorf("%w: missing kid", ErrMalformed)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("%w: signature encoding", ErrMalformed)
	}

	pub, err := ks.Lookup(hdr.Kid, now, leeway)
	if err != nil {
		return nil, err
	}
	if !ed25519.Verify(pub, []byte(parts[0]+"."+parts[1]), sig) {
		return nil, ErrBadSignature
	}

	// Signature is good: only now is the payload trusted enough to parse.
	bodyRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: claims encoding", ErrMalformed)
	}
	var c Claims
	if err := json.Unmarshal(bodyRaw, &c); err != nil {
		return nil, fmt.Errorf("%w: claims json", ErrMalformed)
	}
	if c.Subject == "" || c.JTI == "" || c.ExpiresAt == 0 || c.IssuedAt == 0 {
		return nil, fmt.Errorf("%w: sub, jti, iat and exp are required", ErrMalformed)
	}
	if opts.Issuer != "" && c.Issuer != opts.Issuer {
		return nil, fmt.Errorf("%w: %q", ErrIssuer, c.Issuer)
	}
	if now.Add(-leeway).Unix() >= c.ExpiresAt {
		return nil, ErrExpired
	}
	if now.Add(leeway).Unix() < c.IssuedAt {
		return nil, ErrNotYetValid
	}
	if c.ExpiresAt-c.IssuedAt > int64(maxLife/time.Second) || c.ExpiresAt <= c.IssuedAt {
		return nil, ErrLifetime
	}
	if c.Audience != opts.Audience {
		return nil, ErrAudience
	}
	for _, want := range opts.RequiredScopes {
		if !c.HasScope(want) {
			return nil, fmt.Errorf("%w: %q", ErrScope, want)
		}
	}
	return &c, nil
}
