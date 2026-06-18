package migrations

import "github.com/ambientlabscomputing/underleaf_v2/shared/migrator"

var migration0003 = []string{
	`ALTER TABLE nodes ADD COLUMN ip_address TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE nodes ADD COLUMN os TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE nodes ADD COLUMN arch TEXT NOT NULL DEFAULT '';`,
}

func init() {
	migrator.Migrations = append(migrator.Migrations, migrator.Migration{
		ID:  "0003_node_fields",
		SQL: migration0003,
	})
}
