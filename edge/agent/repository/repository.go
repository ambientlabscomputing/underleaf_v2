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

	// Add other repositories here
	Nodes *NodeRepository
}

func NewRepository() (*Repository, error) {
	cfg := utils.GetConfig(utils.AgentConfig)
	utils.Logger.Debug("Initializing repository with config", "db_path", cfg.DBPath)
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
