package service

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
)

func NewService() Service {
	repo, err := repository.NewRepository()
	if err != nil {
		panic("Failed to initialize repository: " + err.Error())
	}
	if err := repo.Start(); err != nil {
		panic("Failed to start repository: " + err.Error())
	}

	peer, err := clients.NewAgentClient()
	if err != nil {
		panic("orchestrator: failed to create agent gRPC client: " + err.Error())
	}

	cloudClient := clients.NewCloudClient()
	agentClient, err := clients.NewAgentClient()
	if err != nil {
		panic("orchestrator: failed to create agent gRPC client: " + err.Error())
	}

	return &AppService{
		Repository:   repo,
		nodes:        NewNodeService(repo),
		health:       &HealthService{peer: peer},
		containers:   &ContainerService{Repository: repo},
		logs:         &LogService{peer: peer},
		registration: NewRegistrationService(repo.Registration, cloudClient, agentClient),
	}
}

// standalone for CLI commands that don't need the full service stack
func NewNodeService(repo *repository.Repository) *NodeService {
	return &NodeService{Repository: repo}
}
