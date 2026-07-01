package repository

import (
	"database/sql"

	"github.com/doug-martin/goqu/v9"

	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils/sql_utils"
)

type NodeRepository struct {
	db *sql.DB
}

func NewNodeRepository(db *sql.DB) *NodeRepository {
	return &NodeRepository{db: db}
}

func (r *NodeRepository) CreateNode(node *types.Node) error {
	_, err := r.db.Exec(
		"INSERT INTO nodes (id, name, ip_address, os, arch) VALUES (?, ?, ?, ?, ?)",
		node.ID, node.Name, node.IPAddr, node.OS, node.Arch,
	)
	return err
}

func (r *NodeRepository) GetNodeByID(id string) (*types.Node, error) {
	row := r.db.QueryRow("SELECT id, name, ip_address, os, arch FROM nodes WHERE id = ?", id)
	node := &types.Node{}
	err := row.Scan(&node.ID, &node.Name, &node.IPAddr, &node.OS, &node.Arch)
	if err != nil {
		return nil, err
	}
	return node, nil
}

// ListNodes retrieves a list of nodes based on the provided query parameters.
// returns the list of nodes, total count, and any error encountered.
func (r *NodeRepository) ListNodes(query types.QueryNodesRequest) ([]*types.Node, int, error) {
	baseQuery := types.BaseQueryRequest{
		Limit:   query.Limit,
		Offset:  query.Offset,
		OrderBy: query.OrderBy,
		Order:   query.Order,
	}
	statement := sql_utils.BuildBaseQuery("nodes", baseQuery)
	countStatement := sql_utils.BuildBaseCountQuery("nodes", baseQuery)
	if query.Search != "" {
		statement = statement.Where(goqu.I("name").Like("%" + query.Search + "%"))
		countStatement = countStatement.Where(goqu.I("name").Like("%" + query.Search + "%"))
	}
	if query.Name != "" {
		statement = statement.Where(goqu.I("name").Eq(query.Name))
		countStatement = countStatement.Where(goqu.I("name").Eq(query.Name))
	}
	if query.OS != "" {
		statement = statement.Where(goqu.I("os").Eq(query.OS))
		countStatement = countStatement.Where(goqu.I("os").Eq(query.OS))
	}
	if query.Arch != "" {
		statement = statement.Where(goqu.I("arch").Eq(query.Arch))
		countStatement = countStatement.Where(goqu.I("arch").Eq(query.Arch))
	}
	if query.IPAddr != "" {
		statement = statement.Where(goqu.I("ip_address").Eq(query.IPAddr))
		countStatement = countStatement.Where(goqu.I("ip_address").Eq(query.IPAddr))
	}

	stmt, args, err := statement.ToSQL()
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Query(stmt, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var nodes []*types.Node
	for rows.Next() {
		node := &types.Node{}
		if err := rows.Scan(&node.ID, &node.Name, &node.IPAddr, &node.OS, &node.Arch); err != nil {
			return nil, 0, err
		}
		nodes = append(nodes, node)
	}
	countStmt, countArgs, err := countStatement.ToSQL()
	if err != nil {
		return nil, 0, err
	}
	var total int
	err = r.db.QueryRow(countStmt, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	return nodes, total, rows.Err()
}

func (r *NodeRepository) DeleteNode(id string) error {
	_, err := r.db.Exec("DELETE FROM nodes WHERE id = ?", id)
	return err
}
