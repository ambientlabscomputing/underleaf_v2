package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	dockerapi "github.com/docker/docker/api/types"
	dockertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

// Network isolation (RFD 5, step 1).
//
// The Orchestrator container runs on its own user-defined Docker network
// (ControlNetworkName, created by install.sh). Workloads must NOT share it, and
// must not land on Docker's default bridge either, because the default bridge
// is where the Orchestrator used to live and where any other container can
// reach it. Every workload the agent creates is therefore attached to a
// separate user-defined network that the agent creates on demand. Distinct
// user-defined bridge networks are isolated from each other by Docker.
const (
	// EnvWorkloadNetwork overrides the workload network name.
	EnvWorkloadNetwork = "UNDERLEAF_WORKLOAD_NETWORK"
	// DefaultWorkloadNetwork is the user-defined network workloads run on.
	DefaultWorkloadNetwork = "underleaf-workloads"
	// ControlNetworkName is the network install.sh puts the Orchestrator on.
	// The agent refuses to put workloads on it.
	ControlNetworkName = "underleaf-control"

	managedNetworkLabel = "com.ambientlabs.underleaf.managed"
)

// workloadNetworkName returns the configured workload network, or an error if
// it would defeat the isolation (default bridge, host, none, or the
// Orchestrator's own network).
func workloadNetworkName() (string, error) {
	name := strings.TrimSpace(os.Getenv(EnvWorkloadNetwork))
	if name == "" {
		name = DefaultWorkloadNetwork
	}
	switch name {
	case "bridge", "host", "none", "default", ControlNetworkName:
		return "", fmt.Errorf("docker: %s=%q is not allowed: workloads must not share the default bridge, "+
			"the host network, or the Orchestrator's network (%s)", EnvWorkloadNetwork, name, ControlNetworkName)
	}
	if strings.HasPrefix(name, "container:") {
		return "", fmt.Errorf("docker: %s=%q is not allowed: workloads need their own network", EnvWorkloadNetwork, name)
	}
	return name, nil
}

// networkAPI is the slice of the Docker client used to manage the workload
// network; a seam for tests.
type networkAPI interface {
	NetworkInspect(ctx context.Context, networkID string, options dockerapi.NetworkInspectOptions) (dockerapi.NetworkResource, error)
	NetworkCreate(ctx context.Context, name string, options dockerapi.NetworkCreate) (dockerapi.NetworkCreateResponse, error)
}

// ensureNetwork makes sure a user-defined bridge network called name exists.
// An existing network of that name must be a bridge; anything else is refused
// rather than silently reused. Safe to call concurrently and repeatedly.
func ensureNetwork(ctx context.Context, api networkAPI, name string) error {
	check := func() (bool, error) {
		n, err := api.NetworkInspect(ctx, name, dockerapi.NetworkInspectOptions{})
		if err != nil {
			if client.IsErrNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("docker: inspect network %s: %w", name, err)
		}
		if n.Driver != "bridge" {
			return true, fmt.Errorf("docker: network %s exists with driver %q, want a user-defined bridge", name, n.Driver)
		}
		return true, nil
	}

	if found, err := check(); found || err != nil {
		return err
	}
	_, err := api.NetworkCreate(ctx, name, dockerapi.NetworkCreate{
		Driver:         "bridge",
		CheckDuplicate: true,
		Labels:         map[string]string{managedNetworkLabel: "true"},
	})
	if err != nil {
		// Lost a creation race with another caller: fine if it now exists.
		if found, cerr := check(); found && cerr == nil {
			return nil
		}
		return fmt.Errorf("docker: create network %s: %w", name, err)
	}
	return nil
}

// workloadNet lazily creates the workload network once per process.
type workloadNet struct {
	mu   sync.Mutex
	done bool
	name string
}

// ensure returns the workload network name, creating the network if needed.
// Failures are not cached, so a Docker hiccup does not wedge deploys.
func (w *workloadNet) ensure(ctx context.Context, api networkAPI) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done {
		return w.name, nil
	}
	name, err := workloadNetworkName()
	if err != nil {
		return "", err
	}
	if err := ensureNetwork(ctx, api, name); err != nil {
		return "", err
	}
	w.done, w.name = true, name
	return name, nil
}

// workloadHostConfig builds the HostConfig for a workload container. Setting
// NetworkMode here (an empty one means Docker's default bridge) is the whole
// point of step 1.
func workloadHostConfig(volumes []string, portBindings nat.PortMap, networkName string) *dockertypes.HostConfig {
	return &dockertypes.HostConfig{
		Binds:         volumes,
		PortBindings:  portBindings,
		NetworkMode:   dockertypes.NetworkMode(networkName),
		RestartPolicy: dockertypes.RestartPolicy{Name: dockertypes.RestartPolicyUnlessStopped},
		Tmpfs:         map[string]string{"/tmp": "rw,noexec,nosuid,size=64m"},
	}
}
