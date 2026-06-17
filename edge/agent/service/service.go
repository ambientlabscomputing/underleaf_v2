package service

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/repository"
	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/clients"
)

// Service defines the interface for the edge agent service.
type Service interface {
	Start() error
	Stop() error
	Health() *HealthService
	Orchestrator() *OrchestratorService
}

// AppService is the full agent service implementation for the daemon.
type AppService struct {
	health       *HealthService
	orchestrator *OrchestratorService
}

func (s *AppService) Health() *HealthService             { return s.health }
func (s *AppService) Orchestrator() *OrchestratorService { return s.orchestrator }

func (s *AppService) Start() error { return nil }
func (s *AppService) Stop() error  { return nil }

func NewService() Service {
	peer, err := clients.NewOrchestratorClient()
	if err != nil {
		panic("agent: failed to create orchestrator gRPC client: " + err.Error())
	}
	repo, err := repository.NewRepository()
	if err != nil {
		panic("agent: failed to create repository: " + err.Error())
	}
	repo.Start()
	return &AppService{
		health:       &HealthService{peer: peer},
		orchestrator: &OrchestratorService{orchClient: peer, repository: repo},
	}
}
