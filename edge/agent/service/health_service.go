package service

import (
	"context"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
)

// HealthService handles both serving inbound Ping RPCs and initiating outbound
// pings to the orchestrator.
type HealthService struct {
	peer *clients.OrchestratorClient
}

// PingResult is the server-side response produced by this agent when pinged.
type PingResult struct {
	Responder       string
	TimestampUnixMs int64
}

// Ping handles an incoming Ping request from the orchestrator (server-side).
func (h *HealthService) Ping(_ string) (*PingResult, error) {
	return &PingResult{
		Responder:       "agent",
		TimestampUnixMs: time.Now().UnixMilli(),
	}, nil
}

// PingPeer calls the orchestrator's Ping RPC (client-side).
func (h *HealthService) PingPeer(ctx context.Context) (*clients.PingResult, error) {
	return h.peer.Ping(ctx, "agent")
}
