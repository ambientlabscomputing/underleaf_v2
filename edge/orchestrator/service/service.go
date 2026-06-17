package service

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
)

// Service defines the interface for the edge orchestrator service.
type Service interface {
	Start() error
	Stop() error
	Nodes() *NodeService
	Health() *HealthService
}

// This is the heavy full service implementation for the server
type AppService struct {
	Repository *repository.Repository
	nodes      *NodeService
	health     *HealthService
}

func (s *AppService) Nodes() *NodeService    { return s.nodes }
func (s *AppService) Health() *HealthService { return s.health }

func (s *AppService) Start() error {
	// implement start logic
	return nil
}

func (s *AppService) Stop() error {
	// implement stop logic
	return nil
}

func (s *AppService) Query(query string) (string, error) {
	result, err := s.Repository.Query(query)
	return result, err
}
