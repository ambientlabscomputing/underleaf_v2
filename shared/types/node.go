package types

// How a node reaches the Orchestrator (RFD 5, "Two listeners").
const (
	// TransportLocal nodes use the unauthenticated, loopback-only local
	// listener (the single-node install: the manager's own agent).
	TransportLocal = "local"
	// TransportCluster nodes joined with `orcli nodes add` and authenticate
	// with a certificate from the cluster CA on the cluster listener.
	TransportCluster = "cluster"
)

type Node struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	IPAddr string `json:"ip_address"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	// Transport is TransportLocal or TransportCluster. Orchestrator-side only;
	// agents do not store it.
	Transport string `json:"transport,omitempty"`
}

func NewNode(name string) *Node {
	return &Node{
		ID:   GenerateID(NodeIDPrefix),
		Name: name,
	}
}
