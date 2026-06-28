package types

import (
	"encoding/json"
)

type StreamType string

const (
	StreamTypeSSH    StreamType = "ssh"     // make SSH available at <node_id>.ssh.underleafapp.com:22
	StreamTypeGW     StreamType = "gateway" // make a gateway available at https://<conn_id>.gw.underleafapp.com
	StreamTypeTunnel StreamType = "tunnel"  // local in one node <> local in another node
)

type ConnectionState string

const (
	ConnectionStateProvisioned ConnectionState = "provisioned"
	ConnectionStateRunning     ConnectionState = "running"
	ConnectionStateClosed      ConnectionState = "closed"
)

type Connection struct {
	ID        string          `json:"id"`
	NodeID    ForeignKey      `json:"node_id"`
	Name      string          `json:"name"`
	State     ConnectionState `json:"state"`
	Status    Status          `json:"status"`
	CreatedAt string          `json:"created_at"`
	ClosedAt  *string         `json:"closed_at,omitempty"`
}

func (c *Connection) ToJSON() []byte {
	data, err := json.Marshal(c)
	if err != nil {
		return []byte("")
	}
	return data
}

func ConnectionFromJSON(data []byte) (*Connection, error) {
	var conn Connection
	err := json.Unmarshal(data, &conn)
	if err != nil {
		return nil, err
	}
	return &conn, nil
}

type StreamState string

const (
	StreamStateActive StreamState = "active"
	StreamStateClosed StreamState = "closed"
)

type Stream struct {
	ID           string      `json:"id"`
	ConnectionID ForeignKey  `json:"connection_id"`
	Type         StreamType  `json:"type"`
	State        StreamState `json:"state"`
	Status       Status      `json:"status"`
	Endpoint     *string     `json:"endpoint"`
	Port         *int        `json:"port"`
	CreatedAt    string      `json:"created_at"`
	ClosedAt     *string     `json:"closed_at,omitempty"`
}

func (s *Stream) ToJSON() []byte {
	data, err := json.Marshal(s)
	if err != nil {
		return []byte("")
	}
	return data
}

func StreamFromJSON(data []byte) (*Stream, error) {
	var stream Stream
	err := json.Unmarshal(data, &stream)
	if err != nil {
		return nil, err
	}
	return &stream, nil
}
