package repository

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/cli/ui"
	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/migrator"
	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/utils"
)

type Repository struct {
	db       *sql.DB
	Migrator *migrator.Migrator

	// Add other repositories here
	Nodes *NodeRepository
}

func NewRepository() (*Repository, error) {
	cfg := utils.GetConfig(utils.OrchestratorConfig)
	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("repository: open db: %w", err)
	}
	nodeRepo := NewNodeRepository(db)
	return &Repository{
		db:       db,
		Migrator: migrator.NewMigrator(db),
		Nodes:    nodeRepo,
	}, nil
}

func (r *Repository) Start() error {
	return r.Migrator.Migrate()
}

func (r *Repository) Query(query string) (string, error) {
	ui.Debug("Running SQL query: %s", query)
	rows, err := r.db.Query(query)
	if err != nil {
		ui.PrintError("Sql query error: %v", err)
		return "", fmt.Errorf("repository: run sql: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("repository: get columns: %w", err)
	}

	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return "", fmt.Errorf("repository: scan row: %w", err)
		}

		row := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("repository: iterate rows: %w", err)
	}

	ui.Debug("SQL query result: %v", results)
	return fmt.Sprintf("%v", results), nil
}
