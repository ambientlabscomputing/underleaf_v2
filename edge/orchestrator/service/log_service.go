package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// LogService is a pull-through relay: it fetches container logs from the agent
// that runs the container, over that agent's link, without persisting them in
// the orchestrator database.
type LogService struct {
	repo   *repository.Repository
	agents clients.AgentDirectory
}

// LogPage mirrors the agent's paginated result for the REST layer.
type LogPage struct {
	Lines      []*types.LogLine `json:"lines"`
	NextCursor int64            `json:"next_cursor"`
}

// agentFor finds the agent of the node that reported dockerID.
func (s *LogService) agentFor(dockerID string) (clients.AgentAPI, error) {
	c, err := s.repo.Containers.GetContainerByDockerID(dockerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("log_service: no known container with docker id %s", dockerID)
		}
		return nil, fmt.Errorf("log_service: look up container %s: %w", dockerID, err)
	}
	return s.agents.Agent(string(c.NodeID)), nil
}

// GetLogs fetches a page of historical log lines from the agent.
func (s *LogService) GetLogs(ctx context.Context, dockerID string, sinceMs, untilMs int64, limit int, cursorID int64) (*LogPage, error) {
	agent, err := s.agentFor(dockerID)
	if err != nil {
		return nil, err
	}
	resp, err := agent.GetContainerLogs(ctx, dockerID, sinceMs, untilMs, limit, cursorID)
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

// StreamLogs follows a container's logs through a sub-stream of the agent link
// and relays log lines to the returned channel. The channel is closed when the
// stream ends or the context is cancelled.
func (s *LogService) StreamLogs(ctx context.Context, dockerID string, sinceMs int64) (<-chan *types.LogLine, error) {
	agent, err := s.agentFor(dockerID)
	if err != nil {
		return nil, err
	}
	lines, err := agent.StreamContainerLogs(ctx, dockerID, sinceMs)
	if err != nil {
		return nil, fmt.Errorf("log_service: open stream for %s: %w", dockerID, err)
	}

	ch := make(chan *types.LogLine, 256)
	go func() {
		defer close(ch)
		for msg := range lines {
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
