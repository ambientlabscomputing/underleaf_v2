package repository

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository/migrations"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/utils"
)

type Repository struct {
	db       *sql.DB
	Migrator *migrations.Migrator

	// Add other repositories here
	Nodes *NodeRepository
}

func NewRepository() (*Repository, error) {
	cfg := utils.GetConfig()
	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("repository: open db: %w", err)
	}
	nodeRepo := NewNodeRepository(db)
	return &Repository{
		db:       db,
		Migrator: migrations.NewMigrator(db),
		Nodes:    nodeRepo,
	}, nil
}

func (r *Repository) Start() error {
	return r.Migrator.Migrate()
}
