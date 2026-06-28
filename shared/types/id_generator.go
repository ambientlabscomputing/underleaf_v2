package types

import (
	"github.com/rs/xid"
)

type IDPrefix string

const (
	NodeIDPrefix       IDPrefix = "node"
	AppIDPrefix        IDPrefix = "app"
	ContainerIDPrefix  IDPrefix = "container"
	VolumeIDPrefix     IDPrefix = "volume"
	ConnectionIDPrefix IDPrefix = "conn"
	StreamIDPrefix     IDPrefix = "stream"
	RequestIDPrefix    IDPrefix = "req"
)

// GenerateID generates a unique ID with the given prefix.
func GenerateID(prefix IDPrefix) string {
	id := xid.New()
	return string(prefix) + "_" + id.String()
}
