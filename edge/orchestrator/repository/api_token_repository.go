package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrAPITokenNotFound is returned when a token lookup or revoke finds no row.
var ErrAPITokenNotFound = errors.New("api token not found")

// APIToken is a stored API token. It never contains the secret, only its hash.
type APIToken struct {
	ID         string
	Name       string
	Scope      string
	TokenHash  string // hex SHA-256 of the secret
	CreatedAt  time.Time
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	LastUsedAt *time.Time
}

type APITokenRepository struct {
	db *sql.DB
}

func NewAPITokenRepository(db *sql.DB) *APITokenRepository {
	return &APITokenRepository{db: db}
}

// APITokens returns the token repository sharing this repository's database.
func (r *Repository) APITokens() *APITokenRepository {
	return NewAPITokenRepository(r.db)
}

func (r *APITokenRepository) Create(t *APIToken) error {
	_, err := r.db.Exec(
		`INSERT INTO api_tokens (id, name, scope, token_hash, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Scope, t.TokenHash, t.CreatedAt.Unix(), t.ExpiresAt.Unix(),
	)
	return err
}

const apiTokenColumns = `id, name, scope, token_hash, created_at, expires_at, revoked_at, last_used_at`

func (r *APITokenRepository) Get(id string) (*APIToken, error) {
	return scanAPIToken(r.db.QueryRow(`SELECT `+apiTokenColumns+` FROM api_tokens WHERE id = ?`, id))
}

// List returns all tokens, newest first.
func (r *APITokenRepository) List() ([]*APIToken, error) {
	rows, err := r.db.Query(`SELECT ` + apiTokenColumns + ` FROM api_tokens ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, fmt.Errorf("api_token_repo: list: %w", err)
	}
	defer rows.Close()
	var out []*APIToken
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Revoke marks the token revoked. Revoking an already revoked token keeps the
// original revocation time. Unknown ids return ErrAPITokenNotFound.
func (r *APITokenRepository) Revoke(id string, at time.Time) error {
	res, err := r.db.Exec(`UPDATE api_tokens SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, at.Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAPITokenNotFound
	}
	return nil
}

// TouchLastUsed records a use of the token if it was last used before 'before'
// (or never), so frequent requests don't turn into a write each.
func (r *APITokenRepository) TouchLastUsed(id string, at, before time.Time) error {
	_, err := r.db.Exec(
		`UPDATE api_tokens SET last_used_at = ? WHERE id = ? AND (last_used_at IS NULL OR last_used_at < ?)`,
		at.Unix(), id, before.Unix(),
	)
	return err
}

func scanAPIToken(row rowScanner) (*APIToken, error) {
	var t APIToken
	var created, expires int64
	var revoked, lastUsed sql.NullInt64
	err := row.Scan(&t.ID, &t.Name, &t.Scope, &t.TokenHash, &created, &expires, &revoked, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAPITokenNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("api_token_repo: scan: %w", err)
	}
	t.CreatedAt = time.Unix(created, 0)
	t.ExpiresAt = time.Unix(expires, 0)
	if revoked.Valid {
		v := time.Unix(revoked.Int64, 0)
		t.RevokedAt = &v
	}
	if lastUsed.Valid {
		v := time.Unix(lastUsed.Int64, 0)
		t.LastUsedAt = &v
	}
	return &t, nil
}
