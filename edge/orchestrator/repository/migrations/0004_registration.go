package migrations

import "github.com/ambientlabscomputing/underleaf_v2/shared/migrator"

var migration0004 = []string{
	`CREATE TABLE IF NOT EXISTS cluster_registration (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	candidate_id    TEXT NOT NULL,
	cluster_id      TEXT NOT NULL DEFAULT '',
	device_code     TEXT NOT NULL,
	user_code       TEXT NOT NULL,
	verification_uri_complete TEXT NOT NULL,
	status          TEXT NOT NULL DEFAULT 'pending',
	cert_pem        TEXT NOT NULL DEFAULT '',
	key_pem         TEXT NOT NULL DEFAULT '',
	expires_at      INTEGER NOT NULL,
	created_at      INTEGER NOT NULL
);`,
}

func init() {
	migrator.Migrations = append(migrator.Migrations, migrator.Migration{
		ID:  "0004_registration",
		SQL: migration0004,
	})
}
