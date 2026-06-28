package service

func parseIDFromHost(host string, suffix string) string {
	return host[:len(host)-len(suffix)]
}
