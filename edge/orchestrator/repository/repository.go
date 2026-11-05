package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	"modernc.org/sqlite"

	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/ui"
	"github.com/ambientlabscomputing/underleaf_v2/shared/migrator"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

type Repository struct {
	db       *sql.DB
	Migrator *migrator.Migrator

	// Add other repositories here
	Nodes        *NodeRepository
	Containers   *ContainerRepository
	Registration *RegistrationRepository
	Deployments  *DeploymentRepository
}

func NewRepository() (*Repository, error) {
	cfg := utils.GetConfig(utils.OrchestratorConfig)
	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("repository: open db: %w", err)
	}
	nodeRepo := NewNodeRepository(db)
	containerRepo := NewContainerRepository(db)
	registrationRepo := NewRegistrationRepository(db)
	deploymentRepo := NewDeploymentRepository(db)
	return &Repository{
		db:           db,
		Migrator:     migrator.NewMigrator(db),
		Nodes:        nodeRepo,
		Containers:   containerRepo,
		Registration: registrationRepo,
		Deployments:  deploymentRepo,
	}, nil
}

func (r *Repository) Start() error {
	return r.Migrator.Migrate()
}

// sqliteLimitAttached is SQLITE_LIMIT_ATTACHED.
const sqliteLimitAttached = 7

// openReadOnly opens a throwaway connection that cannot modify anything.
// mode=ro stops writes to the database file, but ATTACH DATABASE '<path>' would
// still create/write an arbitrary file, so ATTACH is disabled by capping
// attached databases at zero. (PRAGMA query_only is not enough: a query can
// switch it back off.)
func (r *Repository) openReadOnly(ctx context.Context) (*sql.DB, *sql.Conn, error) {
	cfg := utils.GetConfig(utils.OrchestratorConfig)
	u := url.URL{Opaque: cfg.DBPath, Scheme: "file", RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, nil, err
	}
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	if _, err := sqlite.Limit(conn, sqliteLimitAttached, 0); err != nil {
		conn.Close()
		db.Close()
		return nil, nil, err
	}
	return db, conn, nil
}

// Query runs an ad-hoc read-only SQL query (dev tooling only; see
// grpc_private/sql_dev.go). Writes and ATTACH are rejected.
func (r *Repository) Query(query string) (string, error) {
	ui.Debug("Running SQL query: %s", query)
	db, conn, err := r.openReadOnly(context.Background())
	if err != nil {
		return "", fmt.Errorf("repository: open read-only db: %w", err)
	}
	defer db.Close()
	defer conn.Close()

	rows, err := conn.QueryContext(context.Background(), query)
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
