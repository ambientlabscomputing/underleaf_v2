package types

type Node struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	IPAddr string `json:"ip_address"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
}

func NewNode(name string) *Node {
	return &Node{
		ID:   GenerateID(NodeIDPrefix),
		Name: name,
	}
}
