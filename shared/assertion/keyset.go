package assertion

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// JWK is one public key in the published key set (RFC 8037 OKP/Ed25519, plus
// the validity window the cloud states for rotation).
type JWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	// X is the base64url public key.
	X string `json:"x"`
	// NotBefore and NotAfter are unix seconds. A verifier must not accept the
	// key outside [NotBefore, NotAfter) and may discard it after NotAfter.
	NotBefore int64 `json:"not_before"`
	NotAfter  int64 `json:"not_after"`
}

// KeySet is the published set of assertion verification keys.
type KeySet struct {
	Keys []JWK `json:"keys"`
}

// NewJWK builds the key set entry for an Ed25519 public key.
func NewJWK(kid string, pub ed25519.PublicKey, notBefore, notAfter time.Time) JWK {
	return JWK{
		Kty: "OKP", Crv: "Ed25519", Kid: kid, Alg: Algorithm, Use: "sig",
		X:         base64.RawURLEncoding.EncodeToString(pub),
		NotBefore: notBefore.Unix(), NotAfter: notAfter.Unix(),
	}
}

// Thumbprint returns the RFC 7638 SHA-256 thumbprint of an Ed25519 public
// key. The cloud uses it as the key id, so a kid is bound to its key material.
func Thumbprint(pub ed25519.PublicKey) string {
	x := base64.RawURLEncoding.EncodeToString(pub)
	// RFC 7638: members in lexicographic order, no whitespace.
	canon := `{"crv":"Ed25519","kty":"OKP","x":"` + x + `"}`
	sum := sha256.Sum256([]byte(canon))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// ParseKeySet parses and strictly validates a published key set. Any invalid
// entry (wrong key type or algorithm, bad key length, empty or duplicate kid,
// inverted validity window) rejects the whole document: a verifier that keeps
// its last good copy on a parse error fails closed rather than half-trusting a
// document it cannot fully understand. An empty set is valid (it verifies
// nothing).
func ParseKeySet(data []byte) (*KeySet, error) {
	var ks KeySet
	if err := json.Unmarshal(data, &ks); err != nil {
		return nil, fmt.Errorf("assertion: parse key set: %w", err)
	}
	seen := make(map[string]bool, len(ks.Keys))
	for i := range ks.Keys {
		k := &ks.Keys[i]
		if _, err := k.publicKey(); err != nil {
			return nil, fmt.Errorf("assertion: key %d (%q): %w", i, k.Kid, err)
		}
		if seen[k.Kid] {
			return nil, fmt.Errorf("assertion: duplicate kid %q", k.Kid)
		}
		seen[k.Kid] = true
	}
	return &ks, nil
}

// Marshal serializes the key set for publishing or for the on-disk cache.
func (ks *KeySet) Marshal() ([]byte, error) { return json.Marshal(ks) }

// publicKey validates the entry and decodes its key.
func (k *JWK) publicKey() (ed25519.PublicKey, error) {
	switch {
	case k.Kid == "":
		return nil, errors.New("empty kid")
	case k.Kty != "OKP" || k.Crv != "Ed25519":
		return nil, fmt.Errorf("unsupported key type %q/%q", k.Kty, k.Crv)
	case k.Alg != Algorithm:
		return nil, fmt.Errorf("unsupported alg %q", k.Alg)
	case k.Use != "" && k.Use != "sig":
		return nil, fmt.Errorf("unsupported use %q", k.Use)
	case k.NotAfter <= k.NotBefore:
		return nil, errors.New("not_after must be after not_before")
	}
	raw, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, fmt.Errorf("bad x: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key is %d bytes, want %d", len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

// Lookup returns the key with the given kid if it is usable at now (within
// [not_before-leeway, not_after+0)). ErrUnknownKey means no such kid;
// ErrKeyExpired means the kid is known but outside its window.
func (ks *KeySet) Lookup(kid string, now time.Time, leeway time.Duration) (ed25519.PublicKey, error) {
	if ks == nil || len(ks.Keys) == 0 {
		return nil, ErrNoKeys
	}
	for i := range ks.Keys {
		k := &ks.Keys[i]
		if k.Kid != kid {
			continue
		}
		pub, err := k.publicKey()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnknownKey, err)
		}
		if now.Add(leeway).Unix() < k.NotBefore || now.Unix() >= k.NotAfter {
			return nil, ErrKeyExpired
		}
		return pub, nil
	}
	return nil, ErrUnknownKey
}

// Prune returns a copy of the set without keys whose not_after has passed at
// now. A verifier calls it before persisting its cache so retired keys do not
// linger.
func (ks *KeySet) Prune(now time.Time) *KeySet {
	out := &KeySet{Keys: []JWK{}}
	for _, k := range ks.Keys {
		if now.Unix() < k.NotAfter {
			out.Keys = append(out.Keys, k)
		}
	}
	return out
}
