package clients

// PingResult is the shared response type returned by both OrchestratorClient.Ping
// and AgentClient.Ping.
type PingResult struct {
	Responder       string
	TimestampUnixMs int64
}
