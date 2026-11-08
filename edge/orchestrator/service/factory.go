package service

import (
	"context"
	"os"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/link"
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

	// Agents dial the Orchestrator and keep a link open; the hub is the set of
	// live links and the only way commands reach an agent (RFD 5, step 5).
	hub := link.NewHub()

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
	nodes := NewNodeService(repo)

	// The standing remote-access connection: when a manager-local agent links
	// in, make sure the cloud has a "remote-access" connection for its node and
	// tell the agent to hold it. Does nothing unless the remote listener is
	// enabled and the cluster is registered with the cloud.
	remoteConn := NewRemoteConnectionEnsurer(context.Background(), RemoteConnectionConfig{
		Enabled: RemoteAccessEnabled,
		Cloud:   CloudConnectionsFromCerts(config),
		Agents:  hub,
		Linked:  hub.Linked,
	})
	hub.OnLinked(remoteConn.NodeLinked)

	return &AppService{
		Repository:   repo,
		nodes:        nodes,
		health:       &HealthService{agents: hub},
		containers:   &ContainerService{Repository: repo},
		logs:         &LogService{repo: repo, agents: hub},
		registration: NewRegistrationService(repo.Registration, cloudClient, nodes.ManagerNode),
		connections:  NewConnectionService(cloudClient, hub),
		deployments:  &DeploymentService{Repository: repo},
		manifests:    NewManifestService(),
		reconciler:   &ReconcilerService{Repository: repo, Agents: hub},
		links:        hub,
		cluster:      NewClusterService(repo, utils.ClusterStateDir(config, "ca"), utils.OrchestratorClusterPort()),
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
