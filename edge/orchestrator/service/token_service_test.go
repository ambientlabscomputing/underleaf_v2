package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	_ "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository/migrations"
)

// newTestTokenService runs the real migrations on a temp sqlite file and
// returns a token service over it, a controllable clock, and the db path.
func newTestTokenService(t *testing.T) (*TokenService, *time.Time, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "orch.db")
	cfg := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(cfg, []byte("db_path: "+dbPath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNDERLEAF_CONFIG", cfg)
	repo, err := repository.NewRepository()
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Start(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	svc := NewTokenService(repo.APITokens()).WithClock(func() time.Time { return now })
	return svc, &now, dbPath
}

func TestTokenCreateAndAuthenticate(t *testing.T) {
	svc, _, _ := newTestTokenService(t)
	for _, scope := range []Scope{ScopeRead, ScopeDeploy, ScopeAdmin} {
		c, err := svc.Create("ci", scope, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(c.Secret, "ulat_") || len(c.Secret) != 5+12+1+64 {
			t.Fatalf("unexpected token shape %q", c.Secret)
		}
		p, err := svc.Authenticate(context.Background(), c.Secret)
		if err != nil || p.Scope != scope || p.TokenID != c.Info.ID {
			t.Fatalf("scope %s: %+v %v", scope, p, err)
		}
	}
}

func TestTokenSecretIsNeverStoredInPlaintext(t *testing.T) {
	svc, _, dbPath := newTestTokenService(t)
	c, err := svc.Create("plain-check", ScopeAdmin, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.SplitN(strings.TrimPrefix(c.Secret, "ulat_"), "_", 2)[1]

	// 1. The stored value is the SHA-256 of the secret, not the secret.
	sum := sha256.Sum256([]byte(secret))
	if c.Info.TokenHash != hex.EncodeToString(sum[:]) || c.Info.TokenHash == secret {
		t.Fatalf("stored hash %q is not sha256(secret)", c.Info.TokenHash)
	}

	// 2. No column of any row contains the secret or the full token.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, name, scope, token_hash, created_at, expires_at, revoked_at, last_used_at FROM api_tokens`)
	if err != nil {
		t.Fatal(err)
	}
	cols, _ := rows.Columns()
	n := 0
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		for i, v := range vals {
			if s, ok := v.(string); ok && (strings.Contains(s, secret) || strings.Contains(s, c.Secret)) {
				t.Fatalf("column %s holds the secret", cols[i])
			}
		}
		n++
	}
	rows.Close()
	if n != 1 {
		t.Fatalf("want 1 row, got %d", n)
	}

	// 3. Not anywhere in the database files on disk either (incl. WAL).
	db.Close()
	for _, f := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if strings.Contains(string(b), secret) {
			t.Fatalf("%s contains the plaintext secret", f)
		}
	}
	// ... and the listing API doesn't carry it.
	list, err := svc.List()
	if err != nil || len(list) != 1 || strings.Contains(list[0].TokenHash, secret) {
		t.Fatalf("list: %v %v", list, err)
	}
}

func TestTokenAuthenticateRejections(t *testing.T) {
	svc, _, _ := newTestTokenService(t)
	c, err := svc.Create("x", ScopeRead, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	id := c.Info.ID
	validSecret := strings.SplitN(strings.TrimPrefix(c.Secret, "ulat_"), "_", 2)[1]
	flipped := "0" + validSecret[1:]
	if flipped == validSecret {
		flipped = "1" + validSecret[1:]
	}

	cases := map[string]string{
		"empty":                 "",
		"prefix only":           "ulat_",
		"no prefix":             id + "_" + validSecret,
		"wrong prefix":          "xxxx_" + id + "_" + validSecret,
		"bearer word inside":    "Bearer " + c.Secret,
		"wrong secret":          "ulat_" + id + "_" + flipped,
		"unknown id":            "ulat_" + strings.Repeat("a", 12) + "_" + validSecret,
		"secret of another id":  "ulat_" + strings.Repeat("b", 12) + "_" + validSecret,
		"truncated secret":      c.Secret[:len(c.Secret)-1],
		"extra char":            c.Secret + "0",
		"uppercase hex":         strings.ToUpper(c.Secret[:5]) + id + "_" + strings.ToUpper(validSecret),
		"id used as secret":     "ulat_" + id + "_" + strings.Repeat(id, 6)[:64],
		"stored hash as secret": "ulat_" + id + "_" + c.Info.TokenHash,
		"whitespace":            " " + c.Secret,
		"newline":               c.Secret + "\n",
		"sql-ish":               "ulat_' OR 1=1 --_" + validSecret,
	}
	for name, tok := range cases {
		p, err := svc.Authenticate(context.Background(), tok)
		if p != nil || !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("%s: want ErrInvalidToken, got %+v %v", name, p, err)
		}
	}
	// Sanity: the unmodified token still works.
	if _, err := svc.Authenticate(context.Background(), c.Secret); err != nil {
		t.Fatal(err)
	}
}

func TestTokenExpiry(t *testing.T) {
	svc, now, _ := newTestTokenService(t)
	c, err := svc.Create("short", ScopeDeploy, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	base := *now
	at := func(d time.Duration) { *now = base.Add(d) }

	at(9*time.Minute + 59*time.Second)
	if _, err := svc.Authenticate(context.Background(), c.Secret); err != nil {
		t.Fatalf("one second before expiry must work: %v", err)
	}
	at(10 * time.Minute) // exactly at expiry: expired
	if _, err := svc.Authenticate(context.Background(), c.Secret); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("at expiry: want ErrInvalidToken, got %v", err)
	}
	at(24 * time.Hour)
	if _, err := svc.Authenticate(context.Background(), c.Secret); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("after expiry: want ErrInvalidToken, got %v", err)
	}
	list, _ := svc.List()
	if got := TokenStatus(list[0], *now); got != "expired" {
		t.Fatalf("status %q, want expired", got)
	}
}

func TestTokenRevoke(t *testing.T) {
	svc, now, _ := newTestTokenService(t)
	a, _ := svc.Create("a", ScopeAdmin, time.Hour)
	b, _ := svc.Create("b", ScopeRead, time.Hour)

	if err := svc.Revoke(a.Info.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(context.Background(), a.Secret); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("revoked token must fail, got %v", err)
	}
	if _, err := svc.Authenticate(context.Background(), b.Secret); err != nil {
		t.Fatalf("revoking a must not affect b: %v", err)
	}
	// Idempotent, keeps the first revocation time.
	first, _ := svc.repo.Get(a.Info.ID)
	*now = now.Add(time.Minute)
	if err := svc.Revoke(a.Info.ID); err != nil {
		t.Fatal(err)
	}
	second, _ := svc.repo.Get(a.Info.ID)
	if !first.RevokedAt.Equal(*second.RevokedAt) {
		t.Fatal("second revoke must keep the original time")
	}
	if err := svc.Revoke("doesnotexist"); !errors.Is(err, repository.ErrAPITokenNotFound) {
		t.Fatalf("unknown id: want ErrAPITokenNotFound, got %v", err)
	}
	list, _ := svc.List()
	status := map[string]string{}
	for _, tok := range list {
		status[tok.ID] = TokenStatus(tok, *now)
	}
	if status[a.Info.ID] != "revoked" || status[b.Info.ID] != "active" {
		t.Fatalf("statuses: %v", status)
	}
}

func TestTokenCreateValidation(t *testing.T) {
	svc, _, _ := newTestTokenService(t)
	if _, err := svc.Create("", Scope("root"), time.Hour); err == nil {
		t.Fatal("unknown scope must be rejected")
	}
	if _, err := svc.Create("", Scope(""), time.Hour); err == nil {
		t.Fatal("empty scope must be rejected")
	}
	for _, ttl := range []time.Duration{0, -time.Hour, 30 * time.Second, MaxTokenTTL + time.Hour} {
		if _, err := svc.Create("", ScopeRead, ttl); err == nil {
			t.Fatalf("ttl %s must be rejected", ttl)
		}
	}
	if _, err := svc.Create(strings.Repeat("x", 65), ScopeRead, time.Hour); err == nil {
		t.Fatal("long name must be rejected")
	}
	if _, err := svc.Create("bad\x00name", ScopeRead, time.Hour); err == nil {
		t.Fatal("control chars in name must be rejected")
	}
	if _, err := svc.Create("ok", ScopeRead, MaxTokenTTL); err != nil {
		t.Fatalf("max ttl is allowed: %v", err)
	}
}

func TestScopeAllows(t *testing.T) {
	cases := []struct {
		have, need Scope
		want       bool
	}{
		{ScopeRead, ScopeRead, true}, {ScopeRead, ScopeDeploy, false}, {ScopeRead, ScopeAdmin, false},
		{ScopeDeploy, ScopeRead, true}, {ScopeDeploy, ScopeDeploy, true}, {ScopeDeploy, ScopeAdmin, false},
		{ScopeAdmin, ScopeRead, true}, {ScopeAdmin, ScopeDeploy, true}, {ScopeAdmin, ScopeAdmin, true},
		{Scope(""), ScopeRead, false}, {Scope("root"), ScopeRead, false}, {ScopeAdmin, Scope("root"), false},
	}
	for _, tc := range cases {
		if got := tc.have.Allows(tc.need); got != tc.want {
			t.Fatalf("%q.Allows(%q) = %v, want %v", tc.have, tc.need, got, tc.want)
		}
	}
}

func TestTokenLastUsedIsThrottled(t *testing.T) {
	svc, now, _ := newTestTokenService(t)
	c, _ := svc.Create("u", ScopeRead, 24*time.Hour)
	if _, err := svc.Authenticate(context.Background(), c.Secret); err != nil {
		t.Fatal(err)
	}
	first, _ := svc.repo.Get(c.Info.ID)
	if first.LastUsedAt == nil {
		t.Fatal("last_used_at should be recorded")
	}
	*now = now.Add(10 * time.Second)
	_, _ = svc.Authenticate(context.Background(), c.Secret)
	again, _ := svc.repo.Get(c.Info.ID)
	if !again.LastUsedAt.Equal(*first.LastUsedAt) {
		t.Fatal("uses within a minute must not rewrite last_used_at")
	}
	*now = now.Add(2 * time.Minute)
	_, _ = svc.Authenticate(context.Background(), c.Secret)
	later, _ := svc.repo.Get(c.Info.ID)
	if !later.LastUsedAt.After(*first.LastUsedAt) {
		t.Fatal("a later use should update last_used_at")
	}
}
