package repository

import (
	"database/sql"

	"github.com/doug-martin/goqu/v9"

	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
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
	statement := goqu.Insert("nodes").Rows(
		goqu.Record{
			"id":         node.ID,
			"name":       node.Name,
			"ip_address": node.IPAddr,
			"os":         node.OS,
			"arch":       node.Arch,
		},
	).OnConflict(goqu.DoUpdate("id", goqu.Record{
		"name":       node.Name,
		"ip_address": node.IPAddr,
		"os":         node.OS,
		"arch":       node.Arch,
	}))
	sql, args, err := statement.ToSQL()
	if err != nil {
		return err
	}
	_, err = r.db.Exec(sql, args...)
	return err
}

func (r *NodeRepository) GetNode() (*types.Node, error) {
	row := r.db.QueryRow(`SELECT id, name, ip_address, os, arch FROM nodes LIMIT 1`)
	var node types.Node
	err := row.Scan(&node.ID, &node.Name, &node.IPAddr, &node.OS, &node.Arch)
	if err != nil {
		return nil, err
	}
	return &node, nil
}

func (r *NodeRepository) UpdateNode(node *types.Node) error {
	statement := goqu.Update("nodes").Set(
		goqu.Record{
			"name":       node.Name,
			"ip_address": node.IPAddr,
			"os":         node.OS,
			"arch":       node.Arch,
		},
	).Where(goqu.Ex{"id": node.ID})
	sql, args, err := statement.ToSQL()
	if err != nil {
		return err
	}
	_, err = r.db.Exec(sql, args...)
	return err
}
