package service

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
)

func NewService() Service {
	return &AppService{}
}

// standalone for CLI commands that don't need the full service stack
func NewNodeService(repo *repository.Repository) *NodeService {
	return &NodeService{Repository: repo}
}
