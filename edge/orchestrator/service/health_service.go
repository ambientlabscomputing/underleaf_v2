package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
)

// HealthService handles both serving inbound Ping RPCs and commanding agents
// over their links.
type HealthService struct {
	agents clients.AgentDirectory
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

// ErrNoAgents means no node has a live agent link.
var ErrNoAgents = errors.New("no agent is linked to the Orchestrator yet (is ufagentd running and able to reach it? see `orcli nodes ls`)")

// PingPeer pings an agent over its link: the first linked node, by id.
func (h *HealthService) PingPeer(ctx context.Context) (*clients.PingResult, error) {
	nodes := h.agents.LinkedNodes()
	if len(nodes) == 0 {
		return nil, ErrNoAgents
	}
	return h.agents.Agent(nodes[0]).Ping(ctx, "orchestrator")
}

// IngestAgentContainers instructs every linked agent to ingest its Docker
// containers and push the results back to the orchestrator. It returns the
// total found. Nodes without a link are skipped (they sync when they return);
// it fails only if no agent is linked or every linked agent failed.
func (h *HealthService) IngestAgentContainers(ctx context.Context) (int, error) {
	nodes := h.agents.LinkedNodes()
	if len(nodes) == 0 {
		return 0, ErrNoAgents
	}
	total, failed := 0, 0
	var msgs []string
	for _, id := range nodes {
		n, err := h.agents.Agent(id).IngestContainers(ctx)
		if err != nil {
			failed++
			msgs = append(msgs, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		total += n
	}
	if failed == len(nodes) {
		return 0, fmt.Errorf("container ingest failed on every linked node: %s", strings.Join(msgs, "; "))
	}
	return total, nil
}
