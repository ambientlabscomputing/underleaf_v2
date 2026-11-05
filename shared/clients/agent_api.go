package clients

import (
	"context"
	"fmt"

	agentpb "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
)

// AgentAPI is how the Orchestrator commands one agent. There is no network
// address behind it: an implementation sends each call down that node's agent
// link (edge/orchestrator/link) and returns the agent's answer. Errors from
// the agent keep their gRPC status codes (status.Code works on them).
type AgentAPI interface {
	// Ping calls the agent's Ping and returns the result.
	Ping(ctx context.Context, source string) (*PingResult, error)
	// IngestContainers tells the agent to ingest its local Docker containers and
	// sync them to the orchestrator; it returns how many it found.
	IngestContainers(ctx context.Context) (int, error)
	// GetContainerLogs fetches a page of persisted log lines from the agent.
	GetContainerLogs(ctx context.Context, dockerID string, sinceMs, untilMs int64, limit int, cursorID int64) (*agentpb.GetContainerLogsResponse, error)
	// StreamContainerLogs follows a container's logs (replaying history if
	// sinceMs > 0). The channel closes when the stream ends or ctx is cancelled.
	StreamContainerLogs(ctx context.Context, dockerID string, sinceMs int64) (<-chan *agentpb.LogLine, error)
	GetNode(ctx context.Context) (*agentpb.Node, error)
	// NewStream tells the agent to create a new gateway stream for a connection.
	NewStream(ctx context.Context, connectionID, streamID, streamType string, port int) error
	CloseStream(ctx context.Context, streamID string) error
	GetStream(ctx context.Context, streamID string) (*agentpb.StreamState, error)
	ListStreams(ctx context.Context) ([]*agentpb.StreamState, error)
	// CreateVolume creates a named Docker volume. Idempotent.
	CreateVolume(ctx context.Context, name, driver string, labels map[string]string) error
	ListVolumes(ctx context.Context) ([]*agentpb.VolumeInfo, error)
	// CreateContainer resolves the image (pull or build) and creates the
	// container without starting it.
	CreateContainer(ctx context.Context, req CreateContainerRequest) (string, error)
	StartContainer(ctx context.Context, name string) error
	StopContainer(ctx context.Context, name string) error
	RemoveContainer(ctx context.Context, name string) error
}

// AgentDirectory resolves a node to its agent. It is the Orchestrator's only
// way to reach an agent: by link, never by address.
type AgentDirectory interface {
	// Agent returns a handle for commands to nodeID. The handle does not
	// require the link to be up yet; calls fail with *NoLinkError if it is not
	// when the call is made.
	Agent(nodeID string) AgentAPI
	// LinkedNodes lists the node ids that have a live link right now, sorted.
	LinkedNodes() []string
}

// NoLinkError is returned when a command is addressed to a node with no live
// agent link: the node is down, still starting, partitioned, or never joined.
type NoLinkError struct{ NodeID string }

func (e *NoLinkError) Error() string {
	return fmt.Sprintf("node %s has no active agent link (is its ufagentd running and able to reach the Orchestrator? see `orcli nodes ls`)", e.NodeID)
}

// BuildSourceRequest identifies a downloadable archive to build a container
// image from, and where within it the Docker build context lives.
type BuildSourceRequest struct {
	ArchiveURL string
	Context    string
	Dockerfile string
	Args       map[string]string
}

// CreateContainerRequest describes a container to create on the agent.
// Exactly one of Image or Build should be set.
type CreateContainerRequest struct {
	Name        string
	Image       string
	Build       *BuildSourceRequest
	Environment map[string]string
	Ports       []string // "hostPort:containerPort"
	Volumes     []string // "volumeName:/container/path"
	Labels      map[string]string
}
