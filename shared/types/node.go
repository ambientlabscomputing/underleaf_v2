package types

import "github.com/ambientlabscomputing/underleaf_v2/shared/utils"

type Node struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	IPAddr string `json:"ip_address"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
}

func NewNode(name string) *Node {
	return &Node{
		ID:   utils.GenerateID(utils.NodeIDPrefix),
		Name: name,
	}
}
