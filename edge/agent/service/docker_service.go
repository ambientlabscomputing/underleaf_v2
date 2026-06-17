package service

import (
	"context"
	"fmt"

	dockertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/repository"
	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/types"
)

// DockerService ingests the local node's Docker containers into the agent repository
// and syncs them to the orchestrator.
type DockerService struct {
	docker     *client.Client
	repository *repository.Repository
	orchClient *clients.OrchestratorClient
}

// IngestContainers reads running (and stopped) containers from the local Docker daemon,
// persists them to the agent repository, pushes them to the orchestrator, and returns
// the full list.
func (s *DockerService) IngestContainers(ctx context.Context) ([]*types.Container, error) {
	node, err := s.repository.Nodes.GetNode()
	if err != nil {
		return nil, fmt.Errorf("docker: get local node: %w", err)
	}

	dockerContainers, err := s.docker.ContainerList(ctx, dockertypes.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("docker: list containers: %w", err)
	}

	containers := make([]*types.Container, 0, len(dockerContainers))
	for _, dc := range dockerContainers {
		image := dc.Image
		if len(dc.Names) > 0 {
			// Names have a leading slash; the image field is the canonical image reference.
		}
		c := types.NewContainer(dc.ID, image, types.ForeignKey(node.ID))
		c.Status = dc.Status
		c.Uptime = dc.Created // Unix timestamp of container creation (approximates uptime origin)

		if err := s.repository.Containers.UpsertContainer(c); err != nil {
			return nil, fmt.Errorf("docker: upsert container %s: %w", dc.ID[:12], err)
		}
		containers = append(containers, c)
	}

	// Push to orchestrator — best effort; log but don't fail the ingest.
	if err := s.orchClient.ReportContainers(ctx, node.ID, containers); err != nil {
		fmt.Printf("[docker] warn: failed to report containers to orchestrator: %v\n", err)
	}

	return containers, nil
}

// ListContainers returns the containers currently stored in the agent repository.
func (s *DockerService) ListContainers(ctx context.Context) ([]*types.Container, error) {
	return s.repository.Containers.ListContainers()
}
