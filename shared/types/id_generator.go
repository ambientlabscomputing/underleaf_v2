package types

import (
	"github.com/rs/xid"
)

const (
	NodeIDPrefix       = "node"
	AppIDPrefix        = "app"
	ContainerIDPrefix  = "container"
	VolumeIDPrefix     = "volume"
	TunnelIDPrefix     = "tunnel"
	ConnectionIDPrefix = "conn"
)

// GenerateID generates a unique ID with the given prefix.
func GenerateID(prefix string) string {
	id := xid.New()
	return prefix + "_" + id.String()
}
