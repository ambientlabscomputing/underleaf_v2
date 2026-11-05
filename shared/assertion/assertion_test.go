package assertion

import (
	"crypto"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var t0 = time.Unix(1_800_000_000, 0)

const cluster = "cluster_abc"

type testKey struct {
	kid  string
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
}

func newKey(t *testing.T) testKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return testKey{kid: Thumbprint(pub), priv: priv, pub: pub}
}

func (k testKey) jwk(nb, na time.Time) JWK { return NewJWK(k.kid, k.pub, nb, na) }

func (k testKey) signer(t *testing.T) *Signer {
	t.Helper()
	s, err := NewSigner(k.kid, k.priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func claims(now time.Time) Claims {
	return Claims{
		Issuer: DefaultIssuer, Subject: "account_1", Audience: cluster,
		Scopes: []string{ScopeRead, ScopeDeploy}, IssuedAt: now.Unix(),
		ExpiresAt: now.Add(DefaultTTL).Unix(), JTI: "jti-1",
	}
}

func opts(now time.Time, scopes ...string) VerifyOptions {
	return VerifyOptions{Audience: cluster, Issuer: DefaultIssuer, RequiredScopes: scopes, Now: func() time.Time { return now }}
}

func oneKeySet(k testKey) *KeySet {
	return &KeySet{Keys: []JWK{k.jwk(t0.Add(-time.Hour), t0.Add(24*time.Hour))}}
}

func mustSign(t *testing.T, s *Signer, c Claims) string {
	t.Helper()
	tok, err := s.Sign(c)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestVerifyHappyPath(t *testing.T) {
	k := newKey(t)
	tok := mustSign(t, k.signer(t), claims(t0))
	got, err := Verify(tok, oneKeySet(k), opts(t0, ScopeRead))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Subject != "account_1" || got.Audience != cluster || got.JTI != "jti-1" || !got.HasScope(ScopeDeploy) {
		t.Fatalf("unexpected claims: %+v", got)
	}
	if !got.Expiry().Equal(t0.Add(DefaultTTL)) {
		t.Fatalf("Expiry() = %v", got.Expiry())
	}
}

func TestThumbprintMatchesRFC8037Vector(t *testing.T) {
	// RFC 8037 appendix A.3.
	x, _ := base64.RawURLEncoding.DecodeString("11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo")
	if got := Thumbprint(ed25519.PublicKey(x)); got != "kPrK_qmxVWaYVA9wwBF6Iuo3vVzz7TxHCTwXBygrS4k" {
		t.Fatalf("Thumbprint = %s", got)
	}
}

// Rotation with overlap: the old key stays published (and valid) until its
// stated not_after; new assertions are signed with the new key; after the old
// key's expiry its tokens are rejected while the new key keeps working.
func TestRotationWithOverlap(t *testing.T) {
	oldK, newK := newKey(t), newKey(t)
	rotatedAt := t0.Add(time.Hour)
	oldNotAfter := rotatedAt.Add(2 * time.Hour) // overlap
	ks := &KeySet{Keys: []JWK{
		oldK.jwk(t0.Add(-24*time.Hour), oldNotAfter),
		newK.jwk(rotatedAt, rotatedAt.Add(90*24*time.Hour)),
	}}
	oldTok := mustSign(t, oldK.signer(t), claims(rotatedAt.Add(-time.Second)))
	newTok := mustSign(t, newK.signer(t), claims(rotatedAt))

	// During the overlap both verify. The old token's own exp has passed, so
	// check a token minted by the old key just before the cut-over at a time
	// when it is still fresh.
	if _, err := Verify(oldTok, ks, opts(rotatedAt)); err != nil {
		t.Fatalf("old key token during overlap: %v", err)
	}
	if _, err := Verify(newTok, ks, opts(rotatedAt)); err != nil {
		t.Fatalf("new key token during overlap: %v", err)
	}

	// Old key still published but a verifier with only the old key's window
	// closing: a token minted with the old key right before its not_after.
	late := oldNotAfter.Add(-10 * time.Second)
	lateTok := mustSign(t, oldK.signer(t), claims(late))
	if _, err := Verify(lateTok, ks, opts(late)); err != nil {
		t.Fatalf("old key just before not_after: %v", err)
	}

	// After the old key's not_after: rejected as expired key, even though the
	// token's own exp is still in the future and the key is still in the set.
	after := oldNotAfter
	stale := mustSign(t, oldK.signer(t), claims(after))
	if _, err := Verify(stale, ks, opts(after)); !errors.Is(err, ErrKeyExpired) {
		t.Fatalf("old key after not_after: got %v, want ErrKeyExpired", err)
	}
	// ...and after the cache prunes it, it is an unknown key.
	pruned := ks.Prune(after)
	if len(pruned.Keys) != 1 || pruned.Keys[0].Kid != newK.kid {
		t.Fatalf("Prune kept %+v", pruned.Keys)
	}
	if _, err := Verify(stale, pruned, opts(after)); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("old key after prune: got %v, want ErrUnknownKey", err)
	}
	// The new key is unaffected.
	fresh := mustSign(t, newK.signer(t), claims(after))
	if _, err := Verify(fresh, pruned, opts(after)); err != nil {
		t.Fatalf("new key after old expiry: %v", err)
	}
}

func TestKeyNotYetValidIsRejected(t *testing.T) {
	k := newKey(t)
	ks := &KeySet{Keys: []JWK{k.jwk(t0.Add(time.Hour), t0.Add(2*time.Hour))}}
	tok := mustSign(t, k.signer(t), claims(t0))
	if _, err := Verify(tok, ks, opts(t0)); !errors.Is(err, ErrKeyExpired) {
		t.Fatalf("got %v, want ErrKeyExpired", err)
	}
}

func TestUnknownKidAndNoKeys(t *testing.T) {
	k, other := newKey(t), newKey(t)
	tok := mustSign(t, other.signer(t), claims(t0))
	if _, err := Verify(tok, oneKeySet(k), opts(t0)); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("unknown kid: got %v", err)
	}
	for name, ks := range map[string]*KeySet{"nil": nil, "empty": {}} {
		if _, err := Verify(tok, ks, opts(t0)); !errors.Is(err, ErrNoKeys) {
			t.Fatalf("%s key set: got %v, want ErrNoKeys", name, err)
		}
	}
}

// A token that claims another key's kid but is signed by a different key.
func TestKidWithWrongKeyMaterialFailsSignature(t *testing.T) {
	k, attacker := newKey(t), newKey(t)
	forged := mustSign(t, &Signer{kid: k.kid, priv: attacker.priv}, claims(t0))
	if _, err := Verify(forged, oneKeySet(k), opts(t0)); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("got %v, want ErrBadSignature", err)
	}
}

func seg(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}

func TestAlgorithmConfusion(t *testing.T) {
	k := newKey(t)
	ks := oneKeySet(k)
	body := seg(claims(t0))
	hdr := func(alg, typ string) string { return seg(map[string]string{"alg": alg, "typ": typ, "kid": k.kid}) }

	// alg none, with and without a signature segment.
	for _, tok := range []string{
		hdr("none", TokenType) + "." + body + ".",
		hdr("none", TokenType) + "." + body + "." + base64.RawURLEncoding.EncodeToString(make([]byte, 64)),
		hdr("None", TokenType) + "." + body + ".",
	} {
		if _, err := Verify(tok, ks, opts(t0)); !errors.Is(err, ErrAlgorithm) {
			t.Fatalf("alg none variant: got %v, want ErrAlgorithm", err)
		}
	}

	// HMAC using the *public key* as the secret (the classic confusion).
	for _, alg := range []string{"HS256", "HS384", "HS512"} {
		h := hdr(alg, TokenType)
		mac := hmac.New(sha256.New, k.pub)
		mac.Write([]byte(h + "." + body))
		tok := h + "." + body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
		if _, err := Verify(tok, ks, opts(t0)); !errors.Is(err, ErrAlgorithm) {
			t.Fatalf("%s: got %v, want ErrAlgorithm", alg, err)
		}
	}

	// Correct EdDSA signature but a different "typ" (e.g. some other JWT the
	// cloud signs with the same key by mistake) is not an assertion.
	h := hdr("EdDSA", "JWT")
	sig := ed25519.Sign(k.priv, []byte(h+"."+body))
	tok := h + "." + body + "." + base64.RawURLEncoding.EncodeToString(sig)
	if _, err := Verify(tok, ks, opts(t0)); !errors.Is(err, ErrAlgorithm) {
		t.Fatalf("wrong typ: got %v, want ErrAlgorithm", err)
	}

	// crit header is not understood, so must be refused.
	h = seg(map[string]any{"alg": "EdDSA", "typ": TokenType, "kid": k.kid, "crit": []string{"x"}, "x": 1})
	sig = ed25519.Sign(k.priv, []byte(h+"."+body))
	tok = h + "." + body + "." + base64.RawURLEncoding.EncodeToString(sig)
	if _, err := Verify(tok, ks, opts(t0)); !errors.Is(err, ErrAlgorithm) {
		t.Fatalf("crit: got %v, want ErrAlgorithm", err)
	}
}

// A JWT signed with an RSA key (what the cloud's CA/JWT key produces: RS256)
// is rejected on algorithm before any key is consulted, even when it carries a
// kid that exists in the set and well-formed assertion claims.
func TestRS256TokenFromCAStyleKeyIsNotAnAssertion(t *testing.T) {
	k := newKey(t)
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"JWT", TokenType} {
		h := seg(map[string]string{"alg": "RS256", "typ": typ, "kid": k.kid})
		signingInput := h + "." + seg(claims(t0))
		digest := sha256.Sum256([]byte(signingInput))
		sig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		tok := signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
		if _, err := Verify(tok, oneKeySet(k), opts(t0)); !errors.Is(err, ErrAlgorithm) {
			t.Fatalf("typ=%s: got %v, want ErrAlgorithm", typ, err)
		}
	}
}

func TestWrongAlgForKeyInKeySet(t *testing.T) {
	k := newKey(t)
	j := k.jwk(t0.Add(-time.Hour), t0.Add(time.Hour))
	for name, mutate := range map[string]func(*JWK){
		"rsa kty":     func(j *JWK) { j.Kty = "RSA" },
		"wrong crv":   func(j *JWK) { j.Crv = "Ed448" },
		"wrong alg":   func(j *JWK) { j.Alg = "RS256" },
		"wrong use":   func(j *JWK) { j.Use = "enc" },
		"short key":   func(j *JWK) { j.X = base64.RawURLEncoding.EncodeToString([]byte("short")) },
		"bad base64":  func(j *JWK) { j.X = "!!!" },
		"empty kid":   func(j *JWK) { j.Kid = "" },
		"inverted":    func(j *JWK) { j.NotAfter = j.NotBefore },
		"zero window": func(j *JWK) { j.NotBefore, j.NotAfter = 0, 0 },
	} {
		bad := j
		mutate(&bad)
		raw, _ := json.Marshal(KeySet{Keys: []JWK{bad}})
		if _, err := ParseKeySet(raw); err == nil {
			t.Errorf("%s: ParseKeySet accepted an invalid key", name)
		}
	}
	dup, _ := json.Marshal(KeySet{Keys: []JWK{j, j}})
	if _, err := ParseKeySet(dup); err == nil {
		t.Error("duplicate kid accepted")
	}
	if _, err := ParseKeySet([]byte("not json")); err == nil {
		t.Error("garbage accepted")
	}
	if ks, err := ParseKeySet([]byte(`{"keys":[]}`)); err != nil || len(ks.Keys) != 0 {
		t.Errorf("empty set: %v %v", ks, err)
	}
}

func TestKeySetRoundTrip(t *testing.T) {
	k := newKey(t)
	raw, err := oneKeySet(k).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	ks, err := ParseKeySet(raw)
	if err != nil {
		t.Fatal(err)
	}
	tok := mustSign(t, k.signer(t), claims(t0))
	if _, err := Verify(tok, ks, opts(t0)); err != nil {
		t.Fatalf("after round trip: %v", err)
	}
}

func TestClaimChecks(t *testing.T) {
	k := newKey(t)
	s := k.signer(t)
	ks := oneKeySet(k)

	cases := []struct {
		name   string
		mutate func(*Claims)
		now    time.Time
		opt    func(*VerifyOptions)
		want   error
	}{
		{"expired", func(c *Claims) {}, t0.Add(DefaultTTL + time.Minute), nil, ErrExpired},
		{"expired just past leeway", func(c *Claims) {}, t0.Add(DefaultTTL + DefaultLeeway), nil, ErrExpired},
		{"wrong audience", func(c *Claims) { c.Audience = "cluster_other" }, t0, nil, ErrAudience},
		{"empty audience claim", func(c *Claims) { c.Audience = "" }, t0, nil, ErrAudience},
		{"missing scope", func(c *Claims) { c.Scopes = []string{ScopeRead} }, t0, func(o *VerifyOptions) { o.RequiredScopes = []string{ScopeAdmin} }, ErrScope},
		{"no scopes at all", func(c *Claims) { c.Scopes = nil }, t0, func(o *VerifyOptions) { o.RequiredScopes = []string{ScopeRead} }, ErrScope},
		{"wrong issuer", func(c *Claims) { c.Issuer = "someone-else" }, t0, nil, ErrIssuer},
		{"issued in future", func(c *Claims) {
			c.IssuedAt = t0.Add(time.Hour).Unix()
			c.ExpiresAt = t0.Add(time.Hour + DefaultTTL).Unix()
		}, t0, nil, ErrNotYetValid},
		{"lifetime too long", func(c *Claims) { c.ExpiresAt = t0.Add(time.Hour).Unix() }, t0, nil, ErrLifetime},
		{"exp before iat", func(c *Claims) { c.ExpiresAt = c.IssuedAt - 1 }, t0.Add(-2 * time.Second), nil, ErrLifetime},
		{"missing jti", func(c *Claims) { c.JTI = "" }, t0, nil, ErrMalformed},
		{"missing sub", func(c *Claims) { c.Subject = "" }, t0, nil, ErrMalformed},
		{"missing exp", func(c *Claims) { c.ExpiresAt = 0 }, t0, nil, ErrMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := claims(t0)
			tc.mutate(&c)
			o := opts(tc.now)
			if tc.opt != nil {
				tc.opt(&o)
			}
			if got, err := Verify(mustSign(t, s, c), ks, o); !errors.Is(err, tc.want) || got != nil {
				t.Fatalf("got (%v, %v), want %v", got, err, tc.want)
			}
		})
	}

	t.Run("within leeway accepted", func(t *testing.T) {
		if _, err := Verify(mustSign(t, s, claims(t0)), ks, opts(t0.Add(DefaultTTL+DefaultLeeway-time.Second))); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("all required scopes", func(t *testing.T) {
		if _, err := Verify(mustSign(t, s, claims(t0)), ks, opts(t0, ScopeRead, ScopeDeploy)); err != nil {
			t.Fatal(err)
		}
		if _, err := Verify(mustSign(t, s, claims(t0)), ks, opts(t0, ScopeRead, ScopeAdmin)); !errors.Is(err, ErrScope) {
			t.Fatalf("got %v, want ErrScope", err)
		}
	})
	t.Run("audience option is required", func(t *testing.T) {
		o := opts(t0)
		o.Audience = ""
		if _, err := Verify(mustSign(t, s, claims(t0)), ks, o); err == nil {
			t.Fatal("empty expected audience must be an error, not a wildcard")
		}
	})
}

func TestMalformed(t *testing.T) {
	k := newKey(t)
	ks := oneKeySet(k)
	good := mustSign(t, k.signer(t), claims(t0))
	parts := strings.Split(good, ".")

	badSigLen := parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString([]byte("short"))
	notJSONHdr := base64.RawURLEncoding.EncodeToString([]byte("nope")) + "." + parts[1] + "." + parts[2]
	noKid := seg(map[string]string{"alg": "EdDSA", "typ": TokenType}) + "." + parts[1] + "." + parts[2]
	// Valid signature over a payload that is not JSON.
	h := parts[0]
	junk := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	junkTok := h + "." + junk + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(k.priv, []byte(h+"."+junk)))

	for name, tok := range map[string]string{
		"empty":           "",
		"one segment":     "abc",
		"two segments":    parts[0] + "." + parts[1],
		"four segments":   good + ".extra",
		"bad b64 header":  "!!!." + parts[1] + "." + parts[2],
		"header not json": notJSONHdr,
		"no kid":          noKid,
		"padded b64":      parts[0] + "=." + parts[1] + "." + parts[2],
		"bad sig length":  badSigLen,
		"sig not b64":     parts[0] + "." + parts[1] + ".@@@",
		"oversized":       strings.Repeat("a", maxTokenBytes+1),
		"signed junk":     junkTok,
	} {
		got, err := Verify(tok, ks, opts(t0))
		if got != nil || err == nil || !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: got (%v, %v), want ErrMalformed", name, got, err)
		}
	}
}

func TestTamperedPayloadFailsSignature(t *testing.T) {
	k := newKey(t)
	c := claims(t0)
	parts := strings.Split(mustSign(t, k.signer(t), c), ".")
	c.Audience = "cluster_victim"
	c.Scopes = []string{ScopeAdmin}
	tok := parts[0] + "." + seg(c) + "." + parts[2]
	if _, err := Verify(tok, oneKeySet(k), opts(t0)); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("got %v, want ErrBadSignature", err)
	}
}

func TestNewSignerValidation(t *testing.T) {
	if _, err := NewSigner("", make(ed25519.PrivateKey, ed25519.PrivateKeySize)); err == nil {
		t.Error("empty kid accepted")
	}
	if _, err := NewSigner("k", ed25519.PrivateKey("short")); err == nil {
		t.Error("short key accepted")
	}
	if !ValidScope(ScopeAdmin) || ValidScope("root") || ValidScope("") {
		t.Error("ValidScope")
	}
}
