package service

import (
	"os"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

func NewService(config utils.Config) Service {
	repo, err := repository.NewRepository()
	if err != nil {
		panic("Failed to initialize repository: " + err.Error())
	}
	if err := repo.Start(); err != nil {
		panic("Failed to start repository: " + err.Error())
	}

	peer, err := clients.NewAgentClient()
	if err != nil {
		panic("orchestrator: failed to create agent gRPC client: " + err.Error())
	}

	// check if certs have been provisioned, if so, use mTLS client
	var cloudClient *clients.CloudClient
	clusterCertPath := config.CertDir + "/" + types.CertFileNameClusterCert.String()
	nodeCertPath := config.CertDir + "/" + types.CertFileNameNodeCert.String()
	if fileExists(clusterCertPath) || fileExists(nodeCertPath) {
		cloudClient, err = clients.NewCloudClientWithCert(config)
		if err != nil {
			panic("orchestrator: failed to create cloud gRPC client with cert: " + err.Error())
		}
	} else {
		cloudClient = clients.NewCloudClient()
	}
	agentClient, err := clients.NewAgentClient()
	if err != nil {
		panic("orchestrator: failed to create agent gRPC client: " + err.Error())
	}

	return &AppService{
		Repository:   repo,
		nodes:        NewNodeService(repo),
		health:       &HealthService{peer: peer},
		containers:   &ContainerService{Repository: repo},
		logs:         &LogService{peer: peer},
		registration: NewRegistrationService(repo.Registration, cloudClient, agentClient),
		connections:  NewConnectionService(cloudClient, agentClient),
	}
}

// standalone for CLI commands that don't need the full service stack
func NewNodeService(repo *repository.Repository) *NodeService {
	return &NodeService{Repository: repo}
}

func fileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}
