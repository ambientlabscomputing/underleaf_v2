package service

import (
	"context"
	"os"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/repository"
	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/utils"
)

// OrchestratorService for interacting with the orchestrator
type OrchestratorService struct {
	orchClient *clients.OrchestratorClient
	repository *repository.Repository
}

// NewOrchestratorService creates a new OrchestratorService with the given OrchestratorClient.
func NewOrchestratorService(orchClient *clients.OrchestratorClient, repository *repository.Repository) *OrchestratorService {
	return &OrchestratorService{
		orchClient: orchClient,
		repository: repository,
	}
}

// RegisterNode registers this node with the orchestrator using hostname
func (s *OrchestratorService) RegisterNode(ctx context.Context) (*types.Node, error) {
	name, err := os.Hostname()
	if err != nil {
		utils.Logger.Error("failed to get hostname for node registration: " + err.Error())
		return nil, err
	}
	utils.Logger.Info("Registering node with orchestrator: " + name)
	node, err := s.orchClient.CreateNode(ctx, name)
	if err != nil {
		utils.Logger.Error("failed to create node: " + err.Error())
		return nil, err
	}
	utils.Logger.Info("Node registered with orchestrator: " + node.ID)
	if err := s.repository.Nodes.CreateNode(node); err != nil {
		utils.Logger.Error("failed to create node in repository: " + err.Error())
		return nil, err
	}
	return node, nil
}
