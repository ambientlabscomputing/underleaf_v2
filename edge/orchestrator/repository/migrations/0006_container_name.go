package migrations

import "github.com/ambientlabscomputing/underleaf_v2/shared/migrator"

var migration0006 = []string{
	`ALTER TABLE containers ADD COLUMN name TEXT NOT NULL DEFAULT '';`,
}

func init() {
	migrator.Migrations = append(migrator.Migrations, migrator.Migration{
		ID:  "0006_container_name",
		SQL: migration0006,
	})
}
