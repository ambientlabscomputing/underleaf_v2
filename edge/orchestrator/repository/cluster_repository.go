package repository

import (
	"database/sql"
	"errors"
	"time"
)

// Cluster settings keys.
const (
	SettingAdvertiseAddr   = "advertise_addr"   // host:port nodes are told to dial
	SettingListenerEnabled = "listener_enabled" // "1" once `orcli nodes add` has run
)

// ClusterRepository persists cluster-membership state: settings, join tokens,
// issued node certificates and the revocation deny list.
type ClusterRepository struct {
	db *sql.DB
}

func NewClusterRepository(db *sql.DB) *ClusterRepository { return &ClusterRepository{db: db} }

// GetSetting returns a setting, or "" if unset.
func (r *ClusterRepository) GetSetting(key string) (string, error) {
	var v string
	err := r.db.QueryRow(`SELECT value FROM cluster_settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (r *ClusterRepository) SetSetting(key, value string) error {
	_, err := r.db.Exec(`INSERT INTO cluster_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// --- join tokens -------------------------------------------------------------

// CreateJoinToken stores the hash of a new join secret.
func (r *ClusterRepository) CreateJoinToken(secretHash string, now, expiresAt time.Time) error {
	_, err := r.db.Exec(`INSERT INTO join_tokens (secret_hash, created_at, expires_at) VALUES (?, ?, ?)`,
		secretHash, now.Unix(), expiresAt.Unix())
	return err
}

// ConsumeJoinToken atomically marks a token used. It returns true only if the
// token exists, has not been consumed and has not expired: a reused, expired or
// unknown secret all look the same (false) so callers cannot tell them apart.
func (r *ClusterRepository) ConsumeJoinToken(secretHash string, now time.Time, nodeID string) (bool, error) {
	res, err := r.db.Exec(`UPDATE join_tokens SET consumed_at = ?, consumed_by = ?
		WHERE secret_hash = ? AND consumed_at IS NULL AND expires_at > ?`,
		now.Unix(), nodeID, secretHash, now.Unix())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// PendingJoinTokens counts unconsumed, unexpired tokens.
func (r *ClusterRepository) PendingJoinTokens(now time.Time) (int, error) {
	var n int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM join_tokens WHERE consumed_at IS NULL AND expires_at > ?`, now.Unix()).Scan(&n)
	return n, err
}

// PruneJoinTokens deletes tokens that expired (or were used) before cutoff.
func (r *ClusterRepository) PruneJoinTokens(cutoff time.Time) error {
	_, err := r.db.Exec(`DELETE FROM join_tokens WHERE expires_at < ?`, cutoff.Unix())
	return err
}

// JoinTokenRow is exposed for tests that must prove no plaintext is stored.
type JoinTokenRow struct {
	SecretHash string
	ExpiresAt  time.Time
	Consumed   bool
	ConsumedBy string
}

func (r *ClusterRepository) ListJoinTokens() ([]JoinTokenRow, error) {
	rows, err := r.db.Query(`SELECT secret_hash, expires_at, consumed_at IS NOT NULL, consumed_by FROM join_tokens ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JoinTokenRow
	for rows.Next() {
		var t JoinTokenRow
		var exp int64
		if err := rows.Scan(&t.SecretHash, &exp, &t.Consumed, &t.ConsumedBy); err != nil {
			return nil, err
		}
		t.ExpiresAt = time.Unix(exp, 0)
		out = append(out, t)
	}
	return out, rows.Err()
}

// --- node certificates -------------------------------------------------------

type NodeCertRow struct {
	Serial   string
	NodeID   string
	IssuedAt time.Time
	NotAfter time.Time
}

func (r *ClusterRepository) RecordNodeCert(serial, nodeID string, issuedAt, notAfter time.Time) error {
	_, err := r.db.Exec(`INSERT INTO node_certs (serial, node_id, issued_at, not_after) VALUES (?, ?, ?, ?)`,
		serial, nodeID, issuedAt.Unix(), notAfter.Unix())
	return err
}

// NodeCertExists reports whether serial was issued to nodeID by this CA.
func (r *ClusterRepository) NodeCertExists(serial, nodeID string) (bool, error) {
	var n int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM node_certs WHERE serial = ? AND node_id = ?`, serial, nodeID).Scan(&n)
	return n > 0, err
}

func (r *ClusterRepository) ListNodeCerts(nodeID string) ([]NodeCertRow, error) {
	rows, err := r.db.Query(`SELECT serial, node_id, issued_at, not_after FROM node_certs WHERE node_id = ? ORDER BY issued_at`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NodeCertRow
	for rows.Next() {
		var c NodeCertRow
		var iss, na int64
		if err := rows.Scan(&c.Serial, &c.NodeID, &iss, &na); err != nil {
			return nil, err
		}
		c.IssuedAt, c.NotAfter = time.Unix(iss, 0), time.Unix(na, 0)
		out = append(out, c)
	}
	return out, rows.Err()
}

// LatestNotAfter returns, per node id, the latest expiry among its issued
// certificates that are not revoked.
func (r *ClusterRepository) LatestNotAfter() (map[string]time.Time, error) {
	rows, err := r.db.Query(`SELECT c.node_id, MAX(c.not_after) FROM node_certs c
		WHERE c.serial NOT IN (SELECT serial FROM revoked_certs) GROUP BY c.node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var id string
		var na int64
		if err := rows.Scan(&id, &na); err != nil {
			return nil, err
		}
		out[id] = time.Unix(na, 0)
	}
	return out, rows.Err()
}

// DeleteNodeCerts removes the issuance records for a removed node (revoked
// serials stay on the deny list until they expire).
func (r *ClusterRepository) DeleteNodeCerts(nodeID string) error {
	_, err := r.db.Exec(`DELETE FROM node_certs WHERE node_id = ?`, nodeID)
	return err
}

// --- deny list ---------------------------------------------------------------

func (r *ClusterRepository) Revoke(serial, nodeID string, notAfter, now time.Time) error {
	_, err := r.db.Exec(`INSERT INTO revoked_certs (serial, node_id, not_after, revoked_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(serial) DO NOTHING`, serial, nodeID, notAfter.Unix(), now.Unix())
	return err
}

// RevokedSerials returns the live deny list: revoked certificates that have not
// yet expired. Once a certificate is past NotAfter it fails verification on its
// own, so keeping it listed would only grow the table.
func (r *ClusterRepository) RevokedSerials(now time.Time) ([]string, error) {
	rows, err := r.db.Query(`SELECT serial FROM revoked_certs WHERE not_after > ?`, now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// PruneRevoked deletes deny-list entries whose certificate has expired.
func (r *ClusterRepository) PruneRevoked(now time.Time) (int64, error) {
	res, err := r.db.Exec(`DELETE FROM revoked_certs WHERE not_after <= ?`, now.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountRevoked returns the number of deny-list rows (including expired ones not
// yet pruned); used by tests.
func (r *ClusterRepository) CountRevoked() (int, error) {
	var n int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM revoked_certs`).Scan(&n)
	return n, err
}
