package assertion

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
)

// Signer mints assertions with one Ed25519 key. The production issuer is the
// Python cloud_api (cloud/cloud_api/src/cloud_api/lib/assertion_lib.py), which
// produces the same wire format; this implementation exists so Go tests (here
// and in the Orchestrator) can mint tokens, and so interop with the Python
// issuer is checked in both directions by the test vectors in testdata/.
type Signer struct {
	kid  string
	priv ed25519.PrivateKey
}

// NewSigner returns a Signer for the key.
func NewSigner(kid string, priv ed25519.PrivateKey) (*Signer, error) {
	if kid == "" {
		return nil, errors.New("assertion: empty kid")
	}
	if len(priv) != ed25519.PrivateKeySize {
		return nil, errors.New("assertion: bad private key length")
	}
	return &Signer{kid: kid, priv: priv}, nil
}

// Kid returns the key id placed in the header.
func (s *Signer) Kid() string { return s.kid }

// Sign serializes and signs the claims. It does not validate them; Verify is
// the gate.
func (s *Signer) Sign(c Claims) (string, error) {
	hdr, err := json.Marshal(Header{Alg: Algorithm, Typ: TokenType, Kid: s.kid})
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	signingInput := b64(hdr) + "." + b64(body)
	sig := ed25519.Sign(s.priv, []byte(signingInput))
	return signingInput + "." + b64(sig), nil
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
