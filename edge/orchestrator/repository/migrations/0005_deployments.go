package migrations

import "github.com/ambientlabscomputing/underleaf_v2/shared/migrator"

var migration0005 = []string{
	`CREATE TABLE IF NOT EXISTS deployments (
	id                TEXT PRIMARY KEY,
	repo              TEXT NOT NULL,
	ref               TEXT NOT NULL,
	spec_json         TEXT NOT NULL,
	last_applied_json TEXT NOT NULL DEFAULT '',
	created_at        INTEGER NOT NULL,
	updated_at        INTEGER NOT NULL
);`,
}

func init() {
	migrator.Migrations = append(migrator.Migrations, migrator.Migration{
		ID:  "0005_deployments",
		SQL: migration0005,
	})
}
