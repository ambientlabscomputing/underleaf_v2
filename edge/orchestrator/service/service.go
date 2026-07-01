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
	Containers() *ContainerService
	Logs() *LogService
	Registration() *RegistrationService
	Connections() *ConnectionService
}

// This is the heavy full service implementation for the server
type AppService struct {
	Repository   *repository.Repository
	nodes        *NodeService
	health       *HealthService
	containers   *ContainerService
	logs         *LogService
	registration *RegistrationService
	connections  *ConnectionService
}

func (s *AppService) Nodes() *NodeService                { return s.nodes }
func (s *AppService) Health() *HealthService             { return s.health }
func (s *AppService) Containers() *ContainerService      { return s.containers }
func (s *AppService) Logs() *LogService                  { return s.logs }
func (s *AppService) Registration() *RegistrationService { return s.registration }
func (s *AppService) Connections() *ConnectionService    { return s.connections }

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
