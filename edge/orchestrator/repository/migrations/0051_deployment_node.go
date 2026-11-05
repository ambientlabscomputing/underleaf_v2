package migrations

import "github.com/ambientlabscomputing/underleaf_v2/shared/migrator"

// RFD 5 step 5: a deployment records the node it was placed on, so the
// reconciler picks nodes by agent link yet keeps a deployment on the same node
// across redeploys. Empty means not placed yet.
var migration0051 = []string{
	`ALTER TABLE deployments ADD COLUMN node_id TEXT NOT NULL DEFAULT '';`,
}

func init() {
	migrator.Migrations = append(migrator.Migrations, migrator.Migration{
		ID:  "0051_deployment_node",
		SQL: migration0051,
	})
}
