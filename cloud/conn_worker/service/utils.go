package service

import "strings"

// parseIDFromHost returns the id in "<id><suffix>", or "" if host does not end
// with suffix or has no id in front of it. The host comes straight from the
// request, so it must never be assumed to be long enough: slicing a short or
// foreign Host used to panic the whole process.
func parseIDFromHost(host string, suffix string) string {
	if !strings.HasSuffix(host, suffix) {
		return ""
	}
	return host[:len(host)-len(suffix)]
}
