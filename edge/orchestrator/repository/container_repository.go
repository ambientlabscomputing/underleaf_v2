package repository

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// ContainerRepository provides persistence for Docker containers reported by agents.
type ContainerRepository struct {
	db *sql.DB
}

func NewContainerRepository(db *sql.DB) *ContainerRepository {
	return &ContainerRepository{db: db}
}

// ReplaceContainersForNode upserts the given containers and deletes any
// existing row for this node whose docker_id isn't in the list — the
// agent's ingest always reports its complete local inventory, so anything
// missing has genuinely been removed. Runs in one transaction.
func (r *ContainerRepository) ReplaceContainersForNode(nodeID string, containers []*types.Container) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	keepIDs := make([]string, 0, len(containers))
	for _, c := range containers {
		if _, err := tx.Exec(`
			INSERT INTO containers (id, docker_id, node_id, image, status, uptime, name)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(docker_id) DO UPDATE SET
				node_id  = excluded.node_id,
				image    = excluded.image,
				status   = excluded.status,
				uptime   = excluded.uptime,
				name     = excluded.name
		`, c.ID, c.DockerID, string(c.NodeID), c.Image, c.Status, c.Uptime, c.Name); err != nil {
			return err
		}
		keepIDs = append(keepIDs, c.DockerID)
	}

	if len(keepIDs) == 0 {
		if _, err := tx.Exec(`DELETE FROM containers WHERE node_id = ?`, nodeID); err != nil {
			return err
		}
	} else {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keepIDs)), ",")
		args := make([]any, 0, len(keepIDs)+1)
		args = append(args, nodeID)
		for _, id := range keepIDs {
			args = append(args, id)
		}
		query := fmt.Sprintf(`DELETE FROM containers WHERE node_id = ? AND docker_id NOT IN (%s)`, placeholders)
		if _, err := tx.Exec(query, args...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ListContainers returns all containers known to the orchestrator.
func (r *ContainerRepository) ListContainers() ([]*types.Container, error) {
	rows, err := r.db.Query(`SELECT id, docker_id, node_id, image, status, uptime, name FROM containers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []*types.Container
	for rows.Next() {
		var c types.Container
		var nodeID string
		if err := rows.Scan(&c.ID, &c.DockerID, &nodeID, &c.Image, &c.Status, &c.Uptime, &c.Name); err != nil {
			return nil, err
		}
		c.NodeID = types.ForeignKey(nodeID)
		containers = append(containers, &c)
	}
	return containers, rows.Err()
}

// ListContainersByNode returns containers for a specific node.
func (r *ContainerRepository) ListContainersByNode(nodeID string) ([]*types.Container, error) {
	rows, err := r.db.Query(`SELECT id, docker_id, node_id, image, status, uptime, name FROM containers WHERE node_id = ?`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []*types.Container
	for rows.Next() {
		var c types.Container
		var nid string
		if err := rows.Scan(&c.ID, &c.DockerID, &nid, &c.Image, &c.Status, &c.Uptime, &c.Name); err != nil {
			return nil, err
		}
		c.NodeID = types.ForeignKey(nid)
		containers = append(containers, &c)
	}
	return containers, rows.Err()
}
