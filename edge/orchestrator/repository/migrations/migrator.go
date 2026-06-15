package migrations

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Migration represents a single versioned migration. ID matches the filename
// prefix (e.g. "0001_initial"). SQL is an ordered list of statements to run.
type Migration struct {
	ID  string
	SQL []string
}

// Migrations is the ordered registry all migration files append to via init().
var Migrations []Migration

// Models is the registry of Go structs the dev tool diffs against the live DB
// to autogenerate migration files. Models self-register via init() in their
// own files.
var Models []interface{}

// Migrator applies pending migrations to the database.
type Migrator struct {
	db *sql.DB
}

func NewMigrator(db *sql.DB) *Migrator {
	return &Migrator{db: db}
}

// Migrate applies any Migrations whose ID is not yet recorded in
// schema_migrations. Runs each migration's SQL in a single transaction;
// halts and rolls back on the first error.
func (m *Migrator) Migrate() error {
	if err := m.ensureSchemaTable(); err != nil {
		return fmt.Errorf("migrations: ensure schema table: %w", err)
	}

	applied, err := m.appliedIDs()
	if err != nil {
		return fmt.Errorf("migrations: load applied ids: %w", err)
	}

	for _, migration := range Migrations {
		if applied[migration.ID] {
			continue
		}
		if err := m.apply(migration); err != nil {
			return fmt.Errorf("migrations: apply %s: %w", migration.ID, err)
		}
	}
	return nil
}

func (m *Migrator) ensureSchemaTable() error {
	_, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		id         TEXT PRIMARY KEY,
		applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	return err
}

func (m *Migrator) appliedIDs() (map[string]bool, error) {
	rows, err := m.db.Query(`SELECT id FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		applied[id] = true
	}
	return applied, rows.Err()
}

func (m *Migrator) apply(migration Migration) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}

	for _, stmt := range migration.SQL {
		if _, err := tx.Exec(stmt); err != nil {
			tx.Rollback()
			return fmt.Errorf("statement %q: %w", stmt, err)
		}
	}

	if _, err := tx.Exec(`INSERT INTO schema_migrations (id) VALUES (?)`, migration.ID); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}
