package service

import (
	"fmt"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// ContainerService manages container data received from agents.
type ContainerService struct {
	Repository *repository.Repository
}

// ReportContainers replaces the orchestrator's record of a node's
// containers with the given list. An agent's IngestContainers always lists
// its complete local Docker inventory before reporting, so anything for
// this node not present in containers has genuinely been removed (e.g. by
// `docker rm` outside the normal lifecycle RPCs, or a host reboot) and its
// stale row must go too — otherwise the reconciler (Phase 4) would see a
// long-gone container as "observed" and wrongly leave a fresh deploy alone
// instead of creating it.
func (s *ContainerService) ReportContainers(nodeID string, containers []*types.Container) error {
	for _, c := range containers {
		// Ensure the nodeID in the record matches the reporting node.
		c.NodeID = types.ForeignKey(nodeID)
	}
	if err := s.Repository.Containers.ReplaceContainersForNode(nodeID, containers); err != nil {
		return fmt.Errorf("container_service: replace containers for node %s: %w", nodeID, err)
	}
	return nil
}

// GetContainers returns all containers stored in the orchestrator.
func (s *ContainerService) GetContainers() ([]*types.Container, error) {
	return s.Repository.Containers.ListContainers()
}

// GetContainersByNode returns containers for a specific node.
func (s *ContainerService) GetContainersByNode(nodeID string) ([]*types.Container, error) {
	return s.Repository.Containers.ListContainersByNode(nodeID)
}
