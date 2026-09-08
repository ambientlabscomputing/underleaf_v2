package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// Service defines the interface for the edge orchestrator service.
type Service interface {
	Start() error
	Stop() error
	Nodes() *NodeService
	Health() *HealthService
	Containers() *ContainerService
	Logs() *LogService
	Registration() *RegistrationService
	Connections() *ConnectionService
	Deployments() *DeploymentService
	Manifests() *ManifestService
	Reconciler() *ReconcilerService
	DeployFromSource(ctx context.Context, source, ref, token string) (*types.Deployment, error)
}

// This is the heavy full service implementation for the server
type AppService struct {
	Repository   *repository.Repository
	nodes        *NodeService
	health       *HealthService
	containers   *ContainerService
	logs         *LogService
	registration *RegistrationService
	connections  *ConnectionService
	deployments  *DeploymentService
	manifests    *ManifestService
	reconciler   *ReconcilerService
}

func (s *AppService) Nodes() *NodeService                { return s.nodes }
func (s *AppService) Health() *HealthService             { return s.health }
func (s *AppService) Containers() *ContainerService      { return s.containers }
func (s *AppService) Logs() *LogService                  { return s.logs }
func (s *AppService) Registration() *RegistrationService { return s.registration }
func (s *AppService) Connections() *ConnectionService    { return s.connections }
func (s *AppService) Deployments() *DeploymentService    { return s.deployments }
func (s *AppService) Manifests() *ManifestService        { return s.manifests }
func (s *AppService) Reconciler() *ReconcilerService     { return s.reconciler }

func (s *AppService) Start() error {
	// implement start logic
	return nil
}

func (s *AppService) Stop() error {
	// implement stop logic
	return nil
}

func (s *AppService) Query(query string) (string, error) {
	result, err := s.Repository.Query(query)
	return result, err
}

// DeployFromSource resolves a "gh:<owner>/<repo>[@ref]" source into a
// manifest (Phase 2) and persists it as a deployment (Phase 1) — both fast,
// synchronous steps — then returns immediately with status "in_progress"
// while reconciling it onto the agent (Phase 4) in the background. Callers
// poll Deployments().GetDeployment(id) for the outcome (see reconcileAsync).
// It's the single chain both the REST and gRPC-private Deploy handlers
// call, so that logic exists in one place.
//
// A repo maps to at most one deployment: redeploying a repo that's already
// tracked updates that existing record's spec/ref in place rather than
// inserting a new row. Each deployment ID owns its own last-applied
// snapshot (see ReconcilerService), so always creating a fresh row would
// mean every redeploy reconciles against an empty snapshot and just
// "adopts" the already-running containers instead of applying spec
// changes to them.
func (s *AppService) DeployFromSource(ctx context.Context, source, ref, token string) (*types.Deployment, error) {
	resolved, err := s.manifests.Resolve(ctx, source, ref, token)
	if err != nil {
		return nil, err
	}

	repo := fmt.Sprintf("%s/%s", resolved.Owner, resolved.Repo)

	deployment, err := s.deployments.GetDeploymentByRepo(repo)
	switch {
	case errors.Is(err, repository.ErrDeploymentNotFound):
		deployment, err = s.deployments.CreateDeployment(repo, resolved.Ref, resolved.Spec)
		if err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	default:
		if err := s.deployments.UpdateSpec(deployment.ID, resolved.Ref, resolved.Spec); err != nil {
			return nil, err
		}
		deployment.Ref = resolved.Ref
		deployment.Spec = resolved.Spec
		deployment.Status = types.StatusInProgress
		deployment.Error = ""
	}

	go s.reconcileAsync(deployment)

	return deployment, nil
}

// reconcileAsync runs Reconcile detached from the originating request
// (matching RegistrationService's pollLoop lifetime) and records the
// outcome so pollers see it.
func (s *AppService) reconcileAsync(deployment *types.Deployment) {
	ctx := context.Background()
	if err := s.reconciler.Reconcile(ctx, deployment); err != nil {
		_ = s.deployments.UpdateStatus(deployment.ID, types.StatusFailed, err.Error())
		return
	}
	_ = s.deployments.UpdateStatus(deployment.ID, types.StatusSucceeded, "")
}
