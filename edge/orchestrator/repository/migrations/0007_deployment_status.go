package migrations

import "github.com/ambientlabscomputing/underleaf_v2/shared/migrator"

var migration0007 = []string{
	// Default 'succeeded' for pre-migration rows: those all completed
	// synchronously under the old code path, so this is an honest
	// best-guess backfill, not a live concern.
	`ALTER TABLE deployments ADD COLUMN status TEXT NOT NULL DEFAULT 'succeeded';`,
	`ALTER TABLE deployments ADD COLUMN error TEXT NOT NULL DEFAULT '';`,
}

func init() {
	migrator.Migrations = append(migrator.Migrations, migrator.Migration{
		ID:  "0007_deployment_status",
		SQL: migration0007,
	})
}
