package repository

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/migrator"
	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/utils"
)

type Repository struct {
	db       *sql.DB
	Migrator *migrator.Migrator
}

func NewRepository() (*Repository, error) {
	cfg := utils.GetConfig(utils.AgentConfig)
	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("repository: open db: %w", err)
	}
	return &Repository{
		db:       db,
		Migrator: migrator.NewMigrator(db),
	}, nil
}

func (r *Repository) Start() error {
	return r.Migrator.Migrate()
}
