package service

import (
	"context"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/clients"
)

// HealthService handles both serving inbound Ping RPCs and initiating outbound
// pings to the agent.
type HealthService struct {
	peer *clients.AgentClient
}

// PingResult is the server-side response produced by the orchestrator when pinged.
type PingResult struct {
	Responder       string
	TimestampUnixMs int64
}

// Ping handles an incoming Ping request from an agent (server-side).
func (h *HealthService) Ping(_ string) (*PingResult, error) {
	return &PingResult{
		Responder:       "orchestrator",
		TimestampUnixMs: time.Now().UnixMilli(),
	}, nil
}

// PingPeer calls the agent's Ping RPC (client-side).
func (h *HealthService) PingPeer(ctx context.Context) (*clients.PingResult, error) {
	return h.peer.Ping(ctx, "orchestrator")
}
