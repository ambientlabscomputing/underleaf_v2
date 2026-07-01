package service

import (
	"fmt"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

type NodeService struct {
	Repository *repository.Repository
}

func (s *NodeService) GetNodes(query types.QueryNodesRequest) (types.GetNodesResponse, error) {
	nodes, total, err := s.Repository.Nodes.ListNodes(query)
	if err != nil {
		return types.GetNodesResponse{}, err
	}
	return types.GetNodesResponse{
		Count:   len(nodes),
		Total:   total,
		Results: nodes,
		Query:   query,
	}, nil
}

func (s *NodeService) GetNode(id string) (*types.Node, error) {
	return s.Repository.Nodes.GetNodeByID(id)
}

type ErrNodeAlreadyExists error

func NewErrNodeAlreadyExists(err error) ErrNodeAlreadyExists {
	return ErrNodeAlreadyExists(err)
}

func (s *NodeService) CreateNode(req types.CreateNodeRequest) (*types.Node, error) {
	conflicts, _, err := s.Repository.Nodes.ListNodes(types.QueryNodesRequest{
		Name:   req.Name,
		IPAddr: req.IPAddr,
	})
	if err != nil {
		return nil, err
	}
	if len(conflicts) > 0 {
		return nil, NewErrNodeAlreadyExists(
			fmt.Errorf(
				"node with name %s or IP address %s already exists",
				req.Name, req.IPAddr,
			),
		)
	}

	node := types.NewNode(req.Name)
	node.IPAddr = req.IPAddr
	node.OS = req.OS
	node.Arch = req.Arch
	err = s.Repository.Nodes.CreateNode(node)
	if err != nil {
		return nil, err
	}
	return node, nil
}

func (s *NodeService) DeleteNode(id string) error {
	return s.Repository.Nodes.DeleteNode(id)
}
