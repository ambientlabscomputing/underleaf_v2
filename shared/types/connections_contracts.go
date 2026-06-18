package types

type CreateTunnelRequest struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
}

func (t *CreateTunnelRequest) ToTunnel() *Tunnel {
	return &Tunnel{
		ID:     GenerateID(TunnelIDPrefix), // Implement a function to generate unique IDs
		NodeID: ForeignKey(t.NodeID),
		Name:   t.Name,
		State:  TunnelStateProvisioned,
		Status: StatusSucceeded,
	}
}

type BeginConnRequest struct {
	TunnelID string         `json:"tunnel_id"`
	Type     ConnectionType `json:"type"`
	Endpoint *string        `json:"endpoint"`
	Port     *int           `json:"port"`
}

type TerminateConnRequest struct {
	ConnectionID string `json:"connection_id"`
}

type TerminateConnResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}
