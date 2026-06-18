package service

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	dockertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

const logRetention = 24 * time.Hour
const pruneInterval = 1 * time.Hour

// LogCollector tails Docker container logs, persists them to SQLite, and
// exposes an in-memory fan-out for live gRPC streaming.
type LogCollector struct {
	docker     *client.Client
	repository *repository.Repository
	fanout     *logFanout

	mu      sync.Mutex
	tailers map[string]context.CancelFunc // dockerID -> cancel
}

func newLogCollector(docker *client.Client, repo *repository.Repository) *LogCollector {
	return &LogCollector{
		docker:     docker,
		repository: repo,
		fanout:     newLogFanout(),
		tailers:    make(map[string]context.CancelFunc),
	}
}

// Start launches the background prune goroutine. Call once at daemon startup.
func (lc *LogCollector) Start() {
	go lc.pruneLoop()
}

// Reconcile starts tailers for newly running containers and stops tailers for
// containers that are no longer running. Pass the docker_id values of all
// currently running containers.
func (lc *LogCollector) Reconcile(runningDockerIDs []string) {
	runningSet := make(map[string]struct{}, len(runningDockerIDs))
	for _, id := range runningDockerIDs {
		runningSet[id] = struct{}{}
	}

	lc.mu.Lock()
	defer lc.mu.Unlock()

	// Stop tailers for containers no longer running.
	for id, cancel := range lc.tailers {
		if _, ok := runningSet[id]; !ok {
			cancel()
			delete(lc.tailers, id)
		}
	}

	// Start tailers for new running containers.
	for id := range runningSet {
		if _, ok := lc.tailers[id]; !ok {
			ctx, cancel := context.WithCancel(context.Background())
			lc.tailers[id] = cancel
			go lc.tail(ctx, id)
		}
	}
}

// GetLogs returns a page of persisted log lines for a container.
func (lc *LogCollector) GetLogs(p repository.QueryLogsParams) (*repository.LogPage, error) {
	return lc.repository.ContainerLogs.QueryLines(p)
}

// Subscribe returns a channel that receives live log lines for dockerID.
// The caller must call Unsubscribe when done.
func (lc *LogCollector) Subscribe(dockerID string) chan *types.LogLine {
	return lc.fanout.subscribe(dockerID)
}

// Unsubscribe removes the subscriber channel for dockerID.
func (lc *LogCollector) Unsubscribe(dockerID string, ch chan *types.LogLine) {
	lc.fanout.unsubscribe(dockerID, ch)
}

// tail opens a follow stream from the Docker daemon for dockerID, parses each
// line, writes to SQLite, and publishes to the in-memory fan-out.
func (lc *LogCollector) tail(ctx context.Context, dockerID string) {
	// Pick up where we left off so we don't re-import old lines on restart.
	since := ""
	if ts, err := lc.repository.ContainerLogs.LatestTimestamp(dockerID); err == nil && ts > 0 {
		since = time.UnixMilli(ts).UTC().Format(time.RFC3339Nano)
	}

	opts := dockertypes.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: true,
		Since:      since,
	}

	stream, err := lc.docker.ContainerLogs(ctx, dockerID, opts)
	if err != nil {
		fmt.Printf("[logs] warn: failed to open log stream for %s: %v\n", dockerID[:min(12, len(dockerID))], err)
		return
	}
	defer stream.Close()

	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()

	go lc.readLines(ctx, dockerID, "stdout", stdoutR)
	go lc.readLines(ctx, dockerID, "stderr", stderrR)

	if _, err := stdcopy.StdCopy(stdoutW, stderrW, stream); err != nil && ctx.Err() == nil {
		fmt.Printf("[logs] warn: stdcopy error for %s: %v\n", dockerID[:min(12, len(dockerID))], err)
	}
	stdoutW.Close()
	stderrW.Close()
}

// readLines reads lines from r, parses the Docker timestamp prefix, persists,
// and publishes each line to the fan-out.
func (lc *LogCollector) readLines(ctx context.Context, dockerID, stream string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := parseDockerLogLine(dockerID, stream, scanner.Text())
		if err := lc.repository.ContainerLogs.InsertLine(line); err != nil {
			fmt.Printf("[logs] warn: failed to persist log line for %s: %v\n", dockerID[:min(12, len(dockerID))], err)
		}
		lc.fanout.publish(dockerID, line)
	}
}

// pruneLoop periodically removes log lines older than logRetention.
func (lc *LogCollector) pruneLoop() {
	ticker := time.NewTicker(pruneInterval)
	defer ticker.Stop()
	for range ticker.C {
		if err := lc.repository.ContainerLogs.PruneOlderThan(logRetention); err != nil {
			fmt.Printf("[logs] warn: prune error: %v\n", err)
		}
	}
}

// parseDockerLogLine parses the RFC3339Nano timestamp that Docker prepends when
// Timestamps: true is set on ContainerLogs. Falls back to time.Now() on error.
func parseDockerLogLine(dockerID, stream, raw string) *types.LogLine {
	idx := strings.IndexByte(raw, ' ')
	if idx > 0 {
		if ts, err := time.Parse(time.RFC3339Nano, raw[:idx]); err == nil {
			return &types.LogLine{
				DockerID: dockerID,
				TsMs:     ts.UnixMilli(),
				Stream:   stream,
				Message:  raw[idx+1:],
			}
		}
	}
	return &types.LogLine{
		DockerID: dockerID,
		TsMs:     time.Now().UnixMilli(),
		Stream:   stream,
		Message:  raw,
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ── in-memory fan-out ────────────────────────────────────────────────────────

type logFanout struct {
	mu   sync.Mutex
	subs map[string]map[chan *types.LogLine]struct{}
}

func newLogFanout() *logFanout {
	return &logFanout{subs: make(map[string]map[chan *types.LogLine]struct{})}
}

func (f *logFanout) subscribe(dockerID string) chan *types.LogLine {
	ch := make(chan *types.LogLine, 256)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subs[dockerID] == nil {
		f.subs[dockerID] = make(map[chan *types.LogLine]struct{})
	}
	f.subs[dockerID][ch] = struct{}{}
	return ch
}

func (f *logFanout) unsubscribe(dockerID string, ch chan *types.LogLine) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.subs[dockerID], ch)
	close(ch)
}

func (f *logFanout) publish(dockerID string, line *types.LogLine) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for ch := range f.subs[dockerID] {
		select {
		case ch <- line:
		default: // drop if consumer is too slow
		}
	}
}
