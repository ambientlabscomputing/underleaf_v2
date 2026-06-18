package sql_utils

import (
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/doug-martin/goqu/v9"
)

func BuildBaseQuery(table string, query types.BaseQueryRequest) *goqu.SelectDataset {
	statement := goqu.From(table)
	if query.Limit > 0 {
		statement = statement.Limit(uint(query.Limit))
	}
	if query.Offset > 0 {
		statement = statement.Offset(uint(query.Offset))
	}
	if query.OrderBy != "" {
		if query.Order == "desc" {
			statement = statement.Order(goqu.I(query.OrderBy).Desc())
		} else {
			statement = statement.Order(goqu.I(query.OrderBy).Asc())
		}
	}
	return statement
}

func BuildBaseCountQuery(table string, query types.BaseQueryRequest) *goqu.SelectDataset {
	return goqu.From(table).Select(goqu.COUNT("*"))
}
