package repository

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/ambientlabscomputing/underleaf_v2/shared/migrator"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

type Repository struct {
	db       *sql.DB
	Migrator *migrator.Migrator

	// Add other repositories here
	Nodes         *NodeRepository
	Containers    *ContainerRepository
	ContainerLogs *ContainerLogRepository
}

func NewRepository() (*Repository, error) {
	cfg := utils.GetConfig(utils.AgentConfig)
	utils.Logger.Debug("Initializing repository with config", "db_path", cfg.DBPath)
	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("repository: open db: %w", err)
	}

	// SQLite is single-writer. Cap the pool to one connection so concurrent
	// goroutines (e.g. per-container log tailers) take turns instead of
	// racing. WAL mode lets readers proceed concurrently without blocking
	// the writer. busy_timeout makes the driver wait instead of immediately
	// returning SQLITE_BUSY when the write lock is held.
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			return nil, fmt.Errorf("repository: set pragma %q: %w", pragma, err)
		}
	}

	nodeRepo := NewNodeRepository(db)
	containerRepo := NewContainerRepository(db)
	containerLogRepo := NewContainerLogRepository(db)
	return &Repository{
		db:            db,
		Migrator:      migrator.NewMigrator(db),
		Nodes:         nodeRepo,
		Containers:    containerRepo,
		ContainerLogs: containerLogRepo,
	}, nil
}

func (r *Repository) Start() error {
	return r.Migrator.Migrate()
}
