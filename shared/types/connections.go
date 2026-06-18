package types

type ConnectionType string

const (
	ConnectionTypeSSH  ConnectionType = "ssh"
	ConnectionTypeHTTP ConnectionType = "http"
)

type TunnelState string

const (
	TunnelStateProvisioned TunnelState = "provisioned"
	TunnelStateRunning     TunnelState = "running"
	TunnelStateDeleted     TunnelState = "deleted"
)

type Tunnel struct {
	ID        string      `json:"id"`
	NodeID    ForeignKey  `json:"node_id"`
	Name      string      `json:"name"`
	State     TunnelState `json:"state"`
	Status    Status      `json:"status"`
	CreatedAt string      `json:"created_at"`
	ClosedAt  *string     `json:"closed_at,omitempty"`
}

type ConnectionState string

const (
	ConnectionStateActive  ConnectionState = "active"
	ConnectionStateClosed  ConnectionState = "closed"
	ConnectionStateFailed  ConnectionState = "failed"
	ConnectionStateUnknown ConnectionState = "unknown"
)

type Connection struct {
	ID        string          `json:"id"`
	TunnelID  ForeignKey      `json:"tunnel_id"`
	Type      ConnectionType  `json:"type"`
	State     ConnectionState `json:"state"`
	Status    Status          `json:"status"`
	Endpoint  *string         `json:"endpoint"`
	Port      *int            `json:"port"`
	CreatedAt string          `json:"created_at"`
	ClosedAt  *string         `json:"closed_at,omitempty"`
}
