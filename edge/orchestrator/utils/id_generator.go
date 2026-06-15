package utils

import (
	"github.com/rs/xid"
)

const (
	NodeIDPrefix = "node"
)

// GenerateID generates a unique ID with the given prefix.
func GenerateID(prefix string) string {
	id := xid.New()
	return prefix + "_" + id.String()
}
