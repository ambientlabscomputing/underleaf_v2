package repository

import (
	"database/sql"
	"fmt"
	"time"
)

// RegistrationStatus mirrors cloud-api candidate statuses.
const (
	RegistrationStatusPending    = "pending"
	RegistrationStatusApproved   = "approved"
	RegistrationStatusRegistered = "registered" // cert obtained
	RegistrationStatusExpired    = "expired"
	RegistrationStatusFailed     = "failed"
)

// ClusterRegistration holds the local state of an ongoing or completed registration.
type ClusterRegistration struct {
	ID                      int64
	CandidateID             string
	ClusterID               string
	DeviceCode              string
	UserCode                string
	VerificationURIComplete string
	Status                  string
	CertPEM                 string
	KeyPEM                  string
	ExpiresAt               time.Time
	CreatedAt               time.Time
}

type RegistrationRepository struct {
	db *sql.DB
}

func NewRegistrationRepository(db *sql.DB) *RegistrationRepository {
	return &RegistrationRepository{db: db}
}

func (r *RegistrationRepository) Create(reg *ClusterRegistration) error {
	_, err := r.db.Exec(
		`INSERT INTO cluster_registration
		 (candidate_id, cluster_id, device_code, user_code, verification_uri_complete, status, cert_pem, key_pem, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		reg.CandidateID, reg.ClusterID, reg.DeviceCode, reg.UserCode,
		reg.VerificationURIComplete, reg.Status,
		reg.CertPEM, reg.KeyPEM,
		reg.ExpiresAt.Unix(), reg.CreatedAt.Unix(),
	)
	return err
}

func (r *RegistrationRepository) GetLatest() (*ClusterRegistration, error) {
	row := r.db.QueryRow(
		`SELECT id, candidate_id, cluster_id, device_code, user_code, verification_uri_complete,
		        status, cert_pem, key_pem, expires_at, created_at
		 FROM cluster_registration ORDER BY id DESC LIMIT 1`,
	)
	return scanRegistration(row)
}

func (r *RegistrationRepository) GetByDeviceCode(deviceCode string) (*ClusterRegistration, error) {
	row := r.db.QueryRow(
		`SELECT id, candidate_id, cluster_id, device_code, user_code, verification_uri_complete,
		        status, cert_pem, key_pem, expires_at, created_at
		 FROM cluster_registration WHERE device_code = ?`,
		deviceCode,
	)
	return scanRegistration(row)
}

func (r *RegistrationRepository) UpdateStatus(deviceCode, status, clusterID, certPEM, keyPEM string) error {
	_, err := r.db.Exec(
		`UPDATE cluster_registration SET status = ?, cluster_id = ?, cert_pem = ?, key_pem = ?
		 WHERE device_code = ?`,
		status, clusterID, certPEM, keyPEM, deviceCode,
	)
	return err
}

func scanRegistration(row *sql.Row) (*ClusterRegistration, error) {
	var reg ClusterRegistration
	var expiresUnix, createdUnix int64
	err := row.Scan(
		&reg.ID, &reg.CandidateID, &reg.ClusterID, &reg.DeviceCode, &reg.UserCode,
		&reg.VerificationURIComplete, &reg.Status, &reg.CertPEM, &reg.KeyPEM,
		&expiresUnix, &createdUnix,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("registration_repo: scan: %w", err)
	}
	reg.ExpiresAt = time.Unix(expiresUnix, 0)
	reg.CreatedAt = time.Unix(createdUnix, 0)
	return &reg, nil
}
