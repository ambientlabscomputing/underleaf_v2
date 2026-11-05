package migrations

import "github.com/ambientlabscomputing/underleaf_v2/shared/migrator"

// RFD 5 step 4 (cluster membership). The 0050 number is deliberately far from
// the sequential 0001..0007 series so this migration does not collide with
// others developed in parallel; migrations are applied by id, not by order.
//
//   - cluster_settings: key/value (advertised address, listener enabled).
//   - join_tokens: single-use join secrets, stored only as SHA-256 hashes.
//   - node_certs: every node certificate the CA issued (to show expiry and to
//     know which serials to revoke on `nodes rm`).
//   - revoked_certs: the deny list, keyed by serial and pruned once an entry
//     passes its certificate's expiry.
//   - nodes.transport: 'local' (loopback listener, no certificate) or
//     'cluster' (joined with a certificate).
var migration0050 = []string{
	`CREATE TABLE IF NOT EXISTS cluster_settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);`,
	`CREATE TABLE IF NOT EXISTS join_tokens (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	secret_hash  TEXT NOT NULL UNIQUE,
	created_at   INTEGER NOT NULL,
	expires_at   INTEGER NOT NULL,
	consumed_at  INTEGER,
	consumed_by  TEXT NOT NULL DEFAULT ''
);`,
	`CREATE TABLE IF NOT EXISTS node_certs (
	serial     TEXT PRIMARY KEY,
	node_id    TEXT NOT NULL,
	issued_at  INTEGER NOT NULL,
	not_after  INTEGER NOT NULL
);`,
	`CREATE INDEX IF NOT EXISTS idx_node_certs_node ON node_certs (node_id);`,
	`CREATE TABLE IF NOT EXISTS revoked_certs (
	serial      TEXT PRIMARY KEY,
	node_id     TEXT NOT NULL,
	not_after   INTEGER NOT NULL,
	revoked_at  INTEGER NOT NULL
);`,
	`ALTER TABLE nodes ADD COLUMN transport TEXT NOT NULL DEFAULT 'local';`,
}

func init() {
	migrator.Migrations = append(migrator.Migrations, migrator.Migration{
		ID:  "0050_cluster_membership",
		SQL: migration0050,
	})
}
