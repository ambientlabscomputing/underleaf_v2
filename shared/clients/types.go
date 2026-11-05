package clients

// PingResult is the shared response type returned by both OrchestratorClient.Ping
// and AgentAPI.Ping.
type PingResult struct {
	Responder       string
	TimestampUnixMs int64
}
