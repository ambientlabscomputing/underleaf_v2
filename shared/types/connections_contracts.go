package types

type CreateConnectionRequest struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
}

func (t *CreateConnectionRequest) ToConnection() *Connection {
	return &Connection{
		ID:     GenerateID(StreamIDPrefix), // Implement a function to generate unique IDs
		NodeID: ForeignKey(t.NodeID),
		Name:   t.Name,
		State:  ConnectionStateProvisioned,
		Status: StatusSucceeded,
	}
}

type NewStreamRequest struct {
	ConnectionID string     `json:"connection_id"`
	Type         StreamType `json:"type"`
	Port         *int       `json:"port"`
}

func (t *NewStreamRequest) ToStream() *Stream {
	return &Stream{
		ID:           GenerateID(StreamIDPrefix), // Implement a function to generate unique IDs
		ConnectionID: ForeignKey(t.ConnectionID),
		Type:         t.Type,
		State:        StreamStateActive,
		Status:       StatusSucceeded,
		Port:         t.Port,
	}
}

type TerminateConnRequest struct {
	ConnectionID string `json:"connection_id"`
}

type TerminateConnResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type CloseStreamRequest struct {
	StreamID string `json:"stream_id"`
}

type CloseStreamResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type QueryConnectionsRequest struct {
	Name   *string `json:"name,omitempty"`
	NodeID *string `json:"node_id,omitempty"`
	State  *string `json:"state,omitempty"`
	Status *string `json:"status,omitempty"`
}

type QueryConnectionsResponse struct {
	Items []*Connection `json:"items"`
	Total int           `json:"total"`
}
