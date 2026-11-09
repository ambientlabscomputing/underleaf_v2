package migrations

import "github.com/ambientlabscomputing/underleaf_v2/shared/migrator"

// API tokens (RFD 5, step 6). Only a SHA-256 of the secret is stored; the
// secret itself is shown once at creation and never persisted.
var migrationAPITokens = []string{
	`CREATE TABLE IF NOT EXISTS api_tokens (
	id           TEXT PRIMARY KEY,
	name         TEXT NOT NULL DEFAULT '',
	scope        TEXT NOT NULL CHECK (scope IN ('read', 'deploy', 'admin')),
	token_hash   TEXT NOT NULL,
	created_at   INTEGER NOT NULL,
	expires_at   INTEGER NOT NULL,
	revoked_at   INTEGER,
	last_used_at INTEGER
);`,
}

func init() {
	migrator.Migrations = append(migrator.Migrations, migrator.Migration{
		ID:  "0008_api_tokens",
		SQL: migrationAPITokens,
	})
}
