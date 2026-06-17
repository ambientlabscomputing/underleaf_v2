package service

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/clients"
)

// Service defines the interface for the edge agent service.
type Service interface {
	Start() error
	Stop() error
	Health() *HealthService
}

// AppService is the full agent service implementation for the daemon.
type AppService struct {
	health *HealthService
}

func (s *AppService) Health() *HealthService { return s.health }

func (s *AppService) Start() error { return nil }
func (s *AppService) Stop() error  { return nil }

func NewService() Service {
	peer, err := clients.NewOrchestratorClient()
	if err != nil {
		panic("agent: failed to create orchestrator gRPC client: " + err.Error())
	}
	return &AppService{
		health: &HealthService{peer: peer},
	}
}
