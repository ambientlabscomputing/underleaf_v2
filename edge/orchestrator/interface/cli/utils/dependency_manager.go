package utils

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
)

const (
	RequireNodeService = "RequireNodeService"
)

type DependencyManager struct {
	NodeService *service.NodeService
}

func DependencyManagerBuilder(deps ...string) *DependencyManager {
	repo, err := repository.NewRepository()
	if err := repo.Start(); err != nil {
		panic("Failed to start repository: " + err.Error())
	}
	if err != nil {
		panic("Failed to initialize repository: " + err.Error())
	}
	dm := &DependencyManager{}
	for _, dep := range deps {
		switch dep {
		case RequireNodeService:
			dm.NodeService = service.NewNodeService(repo)
		}
	}
	return dm
}
