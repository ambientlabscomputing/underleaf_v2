package types

type VolumeSpec struct {
	Name string `json:"name"`
}

// ContainerSpec represents the specification of a container within an application.
type ContainerSpec struct {
	Image   string       `json:"image"`
	Volumes []VolumeSpec `json:"volumes"`
}

// BuildSpec describes how to build a container image from source, as declared
// by a service's `build:` block in a deployment manifest.
type BuildSpec struct {
	Context    string            `json:"context" yaml:"context"`
	Dockerfile string            `json:"dockerfile" yaml:"dockerfile"`
	Args       map[string]string `json:"args,omitempty" yaml:"args,omitempty"`
}

// ExposeSpec declares that a service's port should be made reachable through
// the Cloud Gateway. Parsed and persisted today; acting on it is track 3/4 work.
type ExposeSpec struct {
	Port     int    `json:"port" yaml:"port"`
	Hostname string `json:"hostname,omitempty" yaml:"hostname,omitempty"`
}

// NetworkSpec declares a Docker network a deployment's services may join.
// Parsed and persisted today; actually creating/connecting networks is
// deferred (see RFDs/RFD-3.md).
type NetworkSpec struct {
	Name   string `json:"name" yaml:"name"`
	Driver string `json:"driver,omitempty" yaml:"driver,omitempty"`
}

// SourceRef identifies where a service's build context was resolved from.
// Populated by the manifest resolver (not part of the manifest file itself)
// for services with a Build block.
type SourceRef struct {
	Owner      string `json:"owner"`
	Repo       string `json:"repo"`
	Ref        string `json:"ref"`
	ArchiveURL string `json:"archive_url"`
}

// ServiceSpec is the desired-state specification of one service within a
// deployment manifest (a repo's .underleaf/deploy.yaml). Field names mirror
// the pre-existing v1 manifest format for backward compatibility with
// already-published manifests (e.g. ambientlabscomputing/n8n).
type ServiceSpec struct {
	Name        string            `json:"name" yaml:"name"`
	Image       string            `json:"image,omitempty" yaml:"image,omitempty"`
	Build       *BuildSpec        `json:"build,omitempty" yaml:"build,omitempty"`
	Ports       []string          `json:"ports,omitempty" yaml:"ports,omitempty"`
	Environment map[string]string `json:"environment,omitempty" yaml:"environment,omitempty"`
	Networks    []string          `json:"networks,omitempty" yaml:"networks,omitempty"`
	Volumes     []string          `json:"volumes,omitempty" yaml:"volumes,omitempty"` // "volume_name:/path"
	Expose      *ExposeSpec       `json:"expose,omitempty" yaml:"expose,omitempty"`

	Source *SourceRef `json:"source,omitempty" yaml:"-"`
}

// DeploymentSpec is the platonic, desired-state representation of a
// deployment manifest: the parsed contents of a repo's .underleaf/deploy.yaml.
type DeploymentSpec struct {
	Version  string        `json:"version" yaml:"version"`
	Name     string        `json:"name" yaml:"name"`
	Slug     string        `json:"slug,omitempty" yaml:"slug,omitempty"`
	Services []ServiceSpec `json:"services" yaml:"services"`
	Networks []NetworkSpec `json:"networks,omitempty" yaml:"networks,omitempty"`
	Volumes  []VolumeSpec  `json:"volumes,omitempty" yaml:"volumes,omitempty"`
}

// Deployment is a tracked instance of a manifest deployed from a source repo.
// Status is the global in_progress/succeeded/failed lifecycle of its most
// recent reconcile (see shared/types/status.go) — there's no separate
// domain-specific "state" here since reconcile is a single atomic step, not
// a multi-stage process worth naming further.
type Deployment struct {
	ID     string         `json:"id"`
	Repo   string         `json:"repo"` // "owner/repo"
	Ref    string         `json:"ref"`
	Spec   DeploymentSpec `json:"spec"`
	Status Status         `json:"status"`
	Error  string         `json:"error,omitempty"`
}

// Container represents a container within an application.
// Containers are the fundamental units of execution in the system, encapsulating the application code and its dependencies.
type Container struct {
	ContainerSpec
	ID       string     `json:"id"`
	DockerID string     `json:"docker_id"` // native Docker container ID
	Name     string     `json:"name"`      // Docker container name, e.g. "n8n-n8n" (no leading slash)
	AppID    ForeignKey `json:"app_id"`
	NodeID   ForeignKey `json:"node_id"`
	Uptime   int64      `json:"uptime"`
	Status   string     `json:"status"`
}

// LogLine represents a single log line emitted by a container.
type LogLine struct {
	RowID    int64  `json:"-"`
	DockerID string `json:"docker_id"`
	TsMs     int64  `json:"ts_ms"`
	Stream   string `json:"stream"` // "stdout" or "stderr"
	Message  string `json:"message"`
}

// NewContainer creates a new Container instance with a unique ID.
func NewContainer(dockerID, image string, nodeID ForeignKey) *Container {
	return &Container{
		ContainerSpec: ContainerSpec{Image: image},
		ID:            GenerateID(ContainerIDPrefix),
		DockerID:      dockerID,
		NodeID:        nodeID,
	}
}

// Volume represents a volume within an application.
// Volumes are used to persist data across container restarts and can be shared between containers.
type Volume struct {
	VolumeSpec
	ID     string     `json:"id"`
	AppID  ForeignKey `json:"app_id"`
	NodeID ForeignKey `json:"node_id"`
}

// NewDeployment creates a new Deployment instance with a unique ID.
func NewDeployment(repo, ref string, spec DeploymentSpec) *Deployment {
	return &Deployment{
		ID:     GenerateID(DeploymentIDPrefix),
		Repo:   repo,
		Ref:    ref,
		Spec:   spec,
		Status: StatusInProgress,
	}
}
