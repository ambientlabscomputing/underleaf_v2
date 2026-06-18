package migrations

import "github.com/ambientlabscomputing/underleaf_v2/shared/migrator"

// migration0001 was generated with:
//
//	go run ./cmd/migrate provision --autogenerate -m "initial"
//
// Review before committing. Remove any DROP COLUMN lines you do not intend.
var migration0001 = []string{
	`CREATE TABLE IF NOT EXISTS nodes (
	id TEXT PRIMARY KEY,
	name TEXT
);`,
}

func init() {
	migrator.Migrations = append(migrator.Migrations, migrator.Migration{
		ID:  "0001_initial",
		SQL: migration0001,
	})
}
