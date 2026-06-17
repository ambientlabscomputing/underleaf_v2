package repository

import (
	"database/sql"

	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/types"
)

// ContainerRepository provides persistence for Docker containers reported by agents.
type ContainerRepository struct {
	db *sql.DB
}

func NewContainerRepository(db *sql.DB) *ContainerRepository {
	return &ContainerRepository{db: db}
}

// UpsertContainer inserts or updates a container keyed by its Docker container ID.
func (r *ContainerRepository) UpsertContainer(c *types.Container) error {
	_, err := r.db.Exec(`
		INSERT INTO containers (id, docker_id, node_id, image, status, uptime)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(docker_id) DO UPDATE SET
			node_id  = excluded.node_id,
			image    = excluded.image,
			status   = excluded.status,
			uptime   = excluded.uptime
	`, c.ID, c.DockerID, string(c.NodeID), c.Image, c.Status, c.Uptime)
	return err
}

// ListContainers returns all containers known to the orchestrator.
func (r *ContainerRepository) ListContainers() ([]*types.Container, error) {
	rows, err := r.db.Query(`SELECT id, docker_id, node_id, image, status, uptime FROM containers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []*types.Container
	for rows.Next() {
		var c types.Container
		var nodeID string
		if err := rows.Scan(&c.ID, &c.DockerID, &nodeID, &c.Image, &c.Status, &c.Uptime); err != nil {
			return nil, err
		}
		c.NodeID = types.ForeignKey(nodeID)
		containers = append(containers, &c)
	}
	return containers, rows.Err()
}

// ListContainersByNode returns containers for a specific node.
func (r *ContainerRepository) ListContainersByNode(nodeID string) ([]*types.Container, error) {
	rows, err := r.db.Query(`SELECT id, docker_id, node_id, image, status, uptime FROM containers WHERE node_id = ?`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []*types.Container
	for rows.Next() {
		var c types.Container
		var nid string
		if err := rows.Scan(&c.ID, &c.DockerID, &nid, &c.Image, &c.Status, &c.Uptime); err != nil {
			return nil, err
		}
		c.NodeID = types.ForeignKey(nid)
		containers = append(containers, &c)
	}
	return containers, rows.Err()
}
