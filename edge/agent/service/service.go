package service

import (
	"github.com/docker/docker/client"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// Service defines the interface for the edge agent service.
type Service interface {
	Start() error
	Stop() error
	Health() *HealthService
	Orchestrator() *OrchestratorService
	Docker() *DockerService
	Logs() *LogCollector
	GetNode() (*types.Node, error)
}

// AppService is the full agent service implementation for the daemon.
type AppService struct {
	health       *HealthService
	orchestrator *OrchestratorService
	docker       *DockerService
	logs         *LogCollector
	repository   *repository.Repository
}

func (s *AppService) Health() *HealthService             { return s.health }
func (s *AppService) Orchestrator() *OrchestratorService { return s.orchestrator }
func (s *AppService) Docker() *DockerService             { return s.docker }
func (s *AppService) Logs() *LogCollector                { return s.logs }
func (s *AppService) GetNode() (*types.Node, error) {
	return s.repository.Node.GetNode()
}
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

	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		panic("agent: failed to create Docker client: " + err.Error())
	}

	logCollector := newLogCollector(dockerClient, repo)
	logCollector.Start()

	return &AppService{
		health:       &HealthService{peer: peer},
		orchestrator: &OrchestratorService{orchClient: peer, repository: repo},
		docker:       &DockerService{docker: dockerClient, repository: repo, orchClient: peer, logs: logCollector},
		logs:         logCollector,
		repository:   repo,
	}
}
