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
	Endpoint     *string    `json:"endpoint"`
	Port         *int       `json:"port"`
}

func (t *NewStreamRequest) ToStream() *Stream {
	return &Stream{
		ID:           GenerateID(StreamIDPrefix), // Implement a function to generate unique IDs
		ConnectionID: ForeignKey(t.ConnectionID),
		Type:         t.Type,
		State:        StreamStateActive,
		Status:       StatusSucceeded,
		Endpoint:     t.Endpoint,
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
