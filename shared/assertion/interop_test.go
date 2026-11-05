package assertion

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

// vector is the file format shared with cloud_api's
// tests/lib/test_assertion_lib.py.
type vector struct {
	Comment  string   `json:"comment"`
	Now      int64    `json:"now"`
	Audience string   `json:"audience"`
	Subject  string   `json:"subject"`
	Scopes   []string `json:"scopes"`
	JWKS     KeySet   `json:"jwks"`
	Token    string   `json:"token"`
}

// The Python issuer (cloud_api AssertionLib) is the production signer; the Go
// verifier here is what the Orchestrator runs. This proves they agree on the
// wire format, header, claims and key encoding.
func TestInteropWithPythonIssuer(t *testing.T) {
	raw, err := os.ReadFile("testdata/python_issued.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vector
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	// Re-parse the key set through the strict parser, as the Orchestrator will.
	ksRaw, _ := json.Marshal(v.JWKS)
	ks, err := ParseKeySet(ksRaw)
	if err != nil {
		t.Fatalf("Python-published key set rejected by ParseKeySet: %v", err)
	}
	now := time.Unix(v.Now, 0)
	o := VerifyOptions{Audience: v.Audience, Issuer: DefaultIssuer, RequiredScopes: v.Scopes, Now: func() time.Time { return now }}
	c, err := Verify(v.Token, ks, o)
	if err != nil {
		t.Fatalf("Go verifier rejected Python-issued assertion: %v", err)
	}
	if c.Subject != v.Subject || !c.HasScope(ScopeDeploy) || c.ExpiresAt-c.IssuedAt != int64(DefaultTTL/time.Second) {
		t.Fatalf("claims mismatch: %+v", c)
	}
	// The kid is the RFC 7638 thumbprint of the key material, in both languages.
	for _, k := range ks.Keys {
		pub, _ := k.publicKey()
		if Thumbprint(pub) != k.Kid {
			t.Fatalf("kid %q != thumbprint %q", k.Kid, Thumbprint(pub))
		}
	}
	// And the negative path holds on real Python output.
	o.Audience = "cluster_other"
	if _, err := Verify(v.Token, ks, o); !errors.Is(err, ErrAudience) {
		t.Fatalf("got %v, want ErrAudience", err)
	}
	later := now.Add(10 * time.Minute)
	o.Audience, o.Now = v.Audience, func() time.Time { return later }
	if _, err := Verify(v.Token, ks, o); !errors.Is(err, ErrExpired) {
		t.Fatalf("got %v, want ErrExpired", err)
	}
}

// Deterministic Go-issued vector (fixed throwaway test seed; Ed25519 signing is
// deterministic) checked in for the Python side to verify. The seed protects
// nothing: it signs only this fixture.
func TestGoIssuedVectorIsCurrent(t *testing.T) {
	seed := bytes.Repeat([]byte{0x07}, ed25519.SeedSize)
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	kid := Thumbprint(pub)
	now := time.Unix(1_790_000_000, 0)
	s, err := NewSigner(kid, priv)
	if err != nil {
		t.Fatal(err)
	}
	c := Claims{
		Issuer: DefaultIssuer, Subject: "acct_go", Audience: "cluster_go",
		Scopes: []string{ScopeAdmin}, IssuedAt: now.Unix(),
		ExpiresAt: now.Add(DefaultTTL).Unix(), JTI: "jti-go-fixture",
	}
	tok, err := s.Sign(c)
	if err != nil {
		t.Fatal(err)
	}
	want := vector{
		Comment:  "Issued by Go shared/assertion Signer with a throwaway fixed-seed key; verified by cloud_api tests. Regenerate: UPDATE_ASSERTION_VECTORS=1 go test ./shared/assertion",
		Now:      now.Unix(),
		Audience: "cluster_go",
		Subject:  "acct_go",
		Scopes:   []string{ScopeAdmin},
		JWKS:     KeySet{Keys: []JWK{NewJWK(kid, pub, now.Add(-time.Hour), now.Add(24*time.Hour))}},
		Token:    tok,
	}
	got, err := json.MarshalIndent(want, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := "testdata/go_issued.json"
	if os.Getenv("UPDATE_ASSERTION_VECTORS") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	have, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(have, got) {
		t.Fatalf("%s is stale; regenerate with UPDATE_ASSERTION_VECTORS=1", path)
	}
	// It must also verify here, so the fixture is not just self-consistent bytes.
	if _, err := Verify(tok, &want.JWKS, VerifyOptions{Audience: "cluster_go", Now: func() time.Time { return now }}); err != nil {
		t.Fatal(err)
	}
}
