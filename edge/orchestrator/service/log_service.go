package service

import (
	"context"
	"fmt"

	agentpb "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// LogService is a pull-through relay: it fetches container logs from the agent
// on demand without persisting them in the orchestrator database.
type LogService struct {
	peer *clients.AgentClient
}

// LogPage mirrors the agent's paginated result for the REST layer.
type LogPage struct {
	Lines      []*types.LogLine `json:"lines"`
	NextCursor int64            `json:"next_cursor"`
}

// GetLogs fetches a page of historical log lines from the agent.
func (s *LogService) GetLogs(ctx context.Context, dockerID string, sinceMs, untilMs int64, limit int, cursorID int64) (*LogPage, error) {
	resp, err := s.peer.GetContainerLogs(ctx, dockerID, sinceMs, untilMs, limit, cursorID)
	if err != nil {
		return nil, fmt.Errorf("log_service: get logs for %s: %w", dockerID, err)
	}
	page := &LogPage{NextCursor: resp.NextCursor}
	for _, l := range resp.Lines {
		page.Lines = append(page.Lines, &types.LogLine{
			DockerID: l.DockerId,
			TsMs:     l.TsMs,
			Stream:   l.Stream,
			Message:  l.Message,
		})
	}
	return page, nil
}

// StreamLogs opens a server-streaming gRPC call to the agent and relays log
// lines to the returned channel. The channel is closed when the stream ends
// or the context is cancelled.
func (s *LogService) StreamLogs(ctx context.Context, dockerID string, sinceMs int64) (<-chan *types.LogLine, error) {
	stream, err := s.peer.StreamContainerLogs(ctx, dockerID, sinceMs)
	if err != nil {
		return nil, fmt.Errorf("log_service: open stream for %s: %w", dockerID, err)
	}

	ch := make(chan *types.LogLine, 256)
	go func() {
		defer close(ch)
		for {
			msg, err := stream.Recv()
			if err != nil {
				return
			}
			select {
			case ch <- &types.LogLine{
				DockerID: msg.DockerId,
				TsMs:     msg.TsMs,
				Stream:   msg.Stream,
				Message:  msg.Message,
			}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

// toAgentPB is a compile-time assert that agentpb is used.
var _ *agentpb.LogLine = nil
