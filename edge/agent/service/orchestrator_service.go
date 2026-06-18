package service

import (
	"context"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// OrchestratorService for interacting with the orchestrator
type OrchestratorService struct {
	orchClient *clients.OrchestratorClient
	repository *repository.Repository
	hostSvc    *HostService
}

// NewOrchestratorService creates a new OrchestratorService with the given OrchestratorClient.
func NewOrchestratorService(orchClient *clients.OrchestratorClient, repository *repository.Repository, hostSvc *HostService) *OrchestratorService {
	return &OrchestratorService{
		orchClient: orchClient,
		repository: repository,
		hostSvc:    hostSvc,
	}
}

// RegisterNode registers this node with the orchestrator using hostname
func (s *OrchestratorService) RegisterNode(ctx context.Context) (*types.Node, error) {
	hostInfo, err := s.hostSvc.GetHostInfo()
	if err != nil {
		utils.Logger.Error("failed to get host info: " + err.Error())
		return nil, err
	}
	utils.Logger.Info("Registering node with orchestrator", "hostInfo", hostInfo)
	node, err := s.orchClient.CreateNode(ctx, types.CreateNodeRequest{
		Name:   hostInfo.Hostname,
		IPAddr: hostInfo.IPAddr,
		OS:     hostInfo.OS,
		Arch:   hostInfo.Arch,
	})
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
