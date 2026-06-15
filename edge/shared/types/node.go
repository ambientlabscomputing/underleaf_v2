package types

import "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/utils"

type Node struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func NewNode(name string) *Node {
	return &Node{
		ID:   utils.GenerateID(utils.NodeIDPrefix),
		Name: name,
	}
}
