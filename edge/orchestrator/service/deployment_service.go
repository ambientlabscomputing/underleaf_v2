package service

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// DeploymentService manages deployments: manifests resolved from a source
// repo and tracked as desired state to reconcile onto agent nodes.
type DeploymentService struct {
	Repository *repository.Repository
}

// CreateDeployment records a new deployment from a resolved manifest.
func (s *DeploymentService) CreateDeployment(repo, ref string, spec types.DeploymentSpec) (*types.Deployment, error) {
	d := types.NewDeployment(repo, ref, spec)
	if err := s.Repository.Deployments.CreateDeployment(d); err != nil {
		return nil, err
	}
	return d, nil
}

// GetDeployment returns a single deployment by ID.
func (s *DeploymentService) GetDeployment(id string) (*types.Deployment, error) {
	return s.Repository.Deployments.GetDeploymentByID(id)
}

// GetDeploymentByRepo returns the tracked deployment for a source repo, if
// one exists (see DeploymentRepository.GetDeploymentByRepo).
func (s *DeploymentService) GetDeploymentByRepo(repo string) (*types.Deployment, error) {
	return s.Repository.Deployments.GetDeploymentByRepo(repo)
}

// UpdateSpec records a new resolved manifest for an existing deployment.
func (s *DeploymentService) UpdateSpec(id, ref string, spec types.DeploymentSpec) error {
	return s.Repository.Deployments.UpdateSpec(id, ref, spec)
}

// GetDeployments returns all known deployments.
func (s *DeploymentService) GetDeployments() ([]*types.Deployment, error) {
	return s.Repository.Deployments.ListDeployments()
}

// DeleteDeployment removes a deployment record.
func (s *DeploymentService) DeleteDeployment(id string) error {
	return s.Repository.Deployments.DeleteDeployment(id)
}

// UpdateStatus records the outcome of a reconcile attempt.
func (s *DeploymentService) UpdateStatus(id string, status types.Status, errMsg string) error {
	return s.Repository.Deployments.UpdateStatus(id, status, errMsg)
}
