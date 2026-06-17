package repository

import (
	"database/sql"

	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/types"
)

type NodeRepository struct {
	db *sql.DB
}

func NewNodeRepository(db *sql.DB) *NodeRepository {
	return &NodeRepository{
		db: db,
	}
}

func (r *NodeRepository) CreateNode(node *types.Node) error {
	// create singleton node since this is the agent
	_, err := r.db.Exec(`
		INSERT INTO nodes (id, name)
		VALUES (?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
	`, node.ID, node.Name)
	return err
}

func (r *NodeRepository) GetNode() (*types.Node, error) {
	row := r.db.QueryRow(`SELECT id, name FROM nodes LIMIT 1`)
	var node types.Node
	err := row.Scan(&node.ID, &node.Name)
	if err != nil {
		return nil, err
	}
	return &node, nil
}

func (r *NodeRepository) UpdateNode(node *types.Node) error {
	_, err := r.db.Exec(`
		UPDATE nodes SET name = ? WHERE id = ?
	`, node.Name, node.ID)
	return err
}
