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

// ReportContainers persists a batch of containers reported by an agent node.
func (s *ContainerService) ReportContainers(nodeID string, containers []*types.Container) error {
	for _, c := range containers {
		// Ensure the nodeID in the record matches the reporting node.
		c.NodeID = types.ForeignKey(nodeID)
		if err := s.Repository.Containers.UpsertContainer(c); err != nil {
			return fmt.Errorf("container_service: upsert %s: %w", c.DockerID, err)
		}
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
