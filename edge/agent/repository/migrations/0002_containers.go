package migrations

import "github.com/ambientlabscomputing/underleaf_v2/shared/migrator"

var migration0002 = []string{
	`CREATE TABLE IF NOT EXISTS containers (
	id        TEXT PRIMARY KEY,
	docker_id TEXT NOT NULL UNIQUE,
	node_id   TEXT NOT NULL,
	image     TEXT NOT NULL,
	status    TEXT NOT NULL,
	uptime    INTEGER NOT NULL DEFAULT 0
);`,
}

func init() {
	migrator.Migrations = append(migrator.Migrations, migrator.Migration{
		ID:  "0002_containers",
		SQL: migration0002,
	})
}
