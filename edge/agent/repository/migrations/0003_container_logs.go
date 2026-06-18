package migrations

import "github.com/ambientlabscomputing/underleaf_v2/shared/migrator"

var migration0003 = []string{
	`CREATE TABLE IF NOT EXISTS container_logs (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	docker_id TEXT    NOT NULL,
	ts_ms     INTEGER NOT NULL,
	stream    TEXT    NOT NULL,
	message   TEXT    NOT NULL
);`,
	`CREATE INDEX IF NOT EXISTS idx_container_logs_docker_ts
	ON container_logs (docker_id, ts_ms);`,
}

func init() {
	migrator.Migrations = append(migrator.Migrations, migrator.Migration{
		ID:  "0003_container_logs",
		SQL: migration0003,
	})
}
