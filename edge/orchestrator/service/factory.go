package service

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
)

func NewService() Service {
	repo, err := repository.NewRepository()
	if err != nil {
		panic("Failed to initialize repository: " + err.Error())
	}
	if err := repo.Start(); err != nil {
		panic("Failed to start repository: " + err.Error())
	}
	return &AppService{
		Repository: repo,
		nodes:      NewNodeService(repo),
	}
}

// standalone for CLI commands that don't need the full service stack
func NewNodeService(repo *repository.Repository) *NodeService {
	return &NodeService{Repository: repo}
}
