package repository

import (
	"database/sql"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/types"
)

type NodeRepository struct {
	db *sql.DB
}

func NewNodeRepository(db *sql.DB) *NodeRepository {
	return &NodeRepository{db: db}
}

func (r *NodeRepository) CreateNode(node *types.Node) error {
	_, err := r.db.Exec("INSERT INTO nodes (id, name) VALUES (?, ?)", node.ID, node.Name)
	return err
}

func (r *NodeRepository) GetNodeByID(id string) (*types.Node, error) {
	row := r.db.QueryRow("SELECT id, name FROM nodes WHERE id = ?", id)
	node := &types.Node{}
	err := row.Scan(&node.ID, &node.Name)
	if err != nil {
		return nil, err
	}
	return node, nil
}

func (r *NodeRepository) ListNodes() ([]*types.Node, error) {
	rows, err := r.db.Query("SELECT id, name FROM nodes")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*types.Node
	for rows.Next() {
		node := &types.Node{}
		if err := rows.Scan(&node.ID, &node.Name); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func (r *NodeRepository) DeleteNode(id string) error {
	_, err := r.db.Exec("DELETE FROM nodes WHERE id = ?", id)
	return err
}
