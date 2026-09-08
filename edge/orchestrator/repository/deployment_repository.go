package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// ErrDeploymentNotFound is returned when a deployment lookup finds no row.
var ErrDeploymentNotFound = errors.New("deployment not found")

// DeploymentRepository provides persistence for deployments (manifests
// resolved from a source repo) and their last-applied reconciliation state.
type DeploymentRepository struct {
	db *sql.DB
}

func NewDeploymentRepository(db *sql.DB) *DeploymentRepository {
	return &DeploymentRepository{db: db}
}

// CreateDeployment persists a new deployment record.
func (r *DeploymentRepository) CreateDeployment(d *types.Deployment) error {
	specJSON, err := json.Marshal(d.Spec)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	_, err = r.db.Exec(`
		INSERT INTO deployments (id, repo, ref, spec_json, last_applied_json, status, error, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', ?, ?, ?, ?)
	`, d.ID, d.Repo, d.Ref, string(specJSON), string(d.Status), d.Error, now, now)
	return err
}

// UpdateStatus records the outcome of a reconcile attempt.
func (r *DeploymentRepository) UpdateStatus(id string, status types.Status, errMsg string) error {
	_, err := r.db.Exec(`
		UPDATE deployments SET status = ?, error = ?, updated_at = ? WHERE id = ?
	`, string(status), errMsg, time.Now().Unix(), id)
	return err
}

// GetDeploymentByID returns a single deployment by ID.
func (r *DeploymentRepository) GetDeploymentByID(id string) (*types.Deployment, error) {
	row := r.db.QueryRow(`SELECT id, repo, ref, spec_json, status, error FROM deployments WHERE id = ?`, id)
	return scanDeployment(row)
}

// GetDeploymentByRepo returns the tracked deployment for a source repo, if
// one exists. A repo maps to at most one deployment — redeploying the same
// repo updates its existing record (see UpdateSpec) rather than creating a
// second one, so the reconciler's last-applied snapshot stays meaningful
// across redeploys instead of resetting every time.
func (r *DeploymentRepository) GetDeploymentByRepo(repo string) (*types.Deployment, error) {
	row := r.db.QueryRow(`SELECT id, repo, ref, spec_json, status, error FROM deployments WHERE repo = ?`, repo)
	return scanDeployment(row)
}

// UpdateSpec records a new resolved manifest for an existing deployment
// (a redeploy of the same repo) and resets it to "in_progress" for the
// reconcile that's about to run. last_applied_json is left untouched so the
// reconciler can still diff against what's actually running.
func (r *DeploymentRepository) UpdateSpec(id, ref string, spec types.DeploymentSpec) error {
	specJSON, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		UPDATE deployments SET ref = ?, spec_json = ?, status = ?, error = '', updated_at = ? WHERE id = ?
	`, ref, string(specJSON), string(types.StatusInProgress), time.Now().Unix(), id)
	return err
}

// ListDeployments returns all known deployments.
func (r *DeploymentRepository) ListDeployments() ([]*types.Deployment, error) {
	rows, err := r.db.Query(`SELECT id, repo, ref, spec_json, status, error FROM deployments ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deployments []*types.Deployment
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		deployments = append(deployments, d)
	}
	return deployments, rows.Err()
}

// DeleteDeployment removes a deployment record.
func (r *DeploymentRepository) DeleteDeployment(id string) error {
	_, err := r.db.Exec(`DELETE FROM deployments WHERE id = ?`, id)
	return err
}

// GetLastApplied returns the last-applied resource snapshot for a deployment,
// as raw JSON. Returns an empty (nil) snapshot if none has been recorded yet.
func (r *DeploymentRepository) GetLastApplied(id string) ([]byte, error) {
	var raw string
	err := r.db.QueryRow(`SELECT last_applied_json FROM deployments WHERE id = ?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDeploymentNotFound
	}
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, nil
	}
	return []byte(raw), nil
}

// SaveLastApplied persists the last-applied resource snapshot for a deployment.
func (r *DeploymentRepository) SaveLastApplied(id string, snapshot []byte) error {
	_, err := r.db.Exec(`
		UPDATE deployments SET last_applied_json = ?, updated_at = ? WHERE id = ?
	`, string(snapshot), time.Now().Unix(), id)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDeployment(row rowScanner) (*types.Deployment, error) {
	var d types.Deployment
	var specJSON, status string
	if err := row.Scan(&d.ID, &d.Repo, &d.Ref, &specJSON, &status, &d.Error); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDeploymentNotFound
		}
		return nil, err
	}
	d.Status = types.Status(status)
	if err := json.Unmarshal([]byte(specJSON), &d.Spec); err != nil {
		return nil, err
	}
	return &d, nil
}
