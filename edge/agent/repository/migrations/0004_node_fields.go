package migrations

import "github.com/ambientlabscomputing/underleaf_v2/shared/migrator"

var migration0004 = []string{
	`ALTER TABLE nodes ADD COLUMN ip_address TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE nodes ADD COLUMN os TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE nodes ADD COLUMN arch TEXT NOT NULL DEFAULT '';`,
}

func init() {
	migrator.Migrations = append(migrator.Migrations, migrator.Migration{
		ID:  "0004_node_fields",
		SQL: migration0004,
	})
}
