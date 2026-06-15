package service

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/types"
)

type NodeService struct {
	Repository *repository.Repository
}

func (s *NodeService) GetNodes() ([]*types.Node, error) {
	return s.Repository.Nodes.ListNodes()
}
