package types

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/utils"
)

type VolumeSpec struct {
	Name string `json:"name"`
}

// ContainerSpec represents the specification of a container within an application.
type ContainerSpec struct {
	Image   string       `json:"image"`
	Volumes []VolumeSpec `json:"volumes"`
}

// AppSpec represents the platonic representation of an application specification.
// It serves as the "desired state" of an application, which can be used to create or update an application in the system.
type AppSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Containers  []ContainerSpec `json:"containers"`
}

// App represents an application in the system.
// Applications are the containers for a set of containers, scripts, network, volume and other resources that are required to run a specific workload.
type App struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Spec AppSpec `json:"spec"`
}

// Container represents a container within an application.
// Containers are the fundamental units of execution in the system, encapsulating the application code and its dependencies.
type Container struct {
	ContainerSpec
	ID       string     `json:"id"`
	DockerID string     `json:"docker_id"` // native Docker container ID
	AppID    ForeignKey `json:"app_id"`
	NodeID   ForeignKey `json:"node_id"`
	Uptime   int64      `json:"uptime"`
	Status   string     `json:"status"`
}

// NewContainer creates a new Container instance with a unique ID.
func NewContainer(dockerID, image string, nodeID ForeignKey) *Container {
	return &Container{
		ContainerSpec: ContainerSpec{Image: image},
		ID:            utils.GenerateID(utils.ContainerIDPrefix),
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

// NewApp creates a new App instance with a unique ID.
func NewApp(name, description string) *App {
	return &App{
		ID:   utils.GenerateID(utils.AppIDPrefix),
		Name: name,
	}
}
