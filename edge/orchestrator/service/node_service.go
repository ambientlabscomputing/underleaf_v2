package service

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/types"
)

type NodeService struct {
	Repository *repository.Repository
}

func (s *NodeService) GetNodes() ([]*types.Node, error) {
	return s.Repository.Nodes.ListNodes()
}

func (s *NodeService) GetNode(id string) (*types.Node, error) {
	return s.Repository.Nodes.GetNodeByID(id)
}

type CreateNodeRequest struct {
	Name string `json:"name"`
}

func (s *NodeService) CreateNode(req CreateNodeRequest) (*types.Node, error) {
	node := types.NewNode(req.Name)
	err := s.Repository.Nodes.CreateNode(node)
	if err != nil {
		return nil, err
	}
	return node, nil
}

func (s *NodeService) DeleteNode(id string) error {
	return s.Repository.Nodes.DeleteNode(id)
}
