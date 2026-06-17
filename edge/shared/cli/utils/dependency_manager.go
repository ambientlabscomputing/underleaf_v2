package utils

import (
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	agent_grpc_private "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_private"
	grpc_private "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_private"
)

const (
	RequirePrivateClient      = "RequirePrivateClient"
	RequireAgentPrivateClient = "RequireAgentPrivateClient"
)

const privateSocketPath = "/tmp/undf-orch.sock"
const agentSocketPath = "/tmp/undf-agent.sock"

// DependencyManager holds gRPC clients for orchestrator CLI commands.
type DependencyManager struct {
	OrchestratorPrivateClient grpc_private.OrchestratorPrivateClient
	privateConn               *grpc.ClientConn
}

func DependencyManagerBuilder(deps ...string) *DependencyManager {
	dm := &DependencyManager{}
	for _, dep := range deps {
		switch dep {
		case RequirePrivateClient:
			conn, err := grpc.NewClient(
				fmt.Sprintf("unix://%s", privateSocketPath),
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			)
			if err != nil {
				panic("Failed to connect to orchestrator: " + err.Error())
			}
			dm.privateConn = conn
			dm.OrchestratorPrivateClient = grpc_private.NewOrchestratorPrivateClient(conn)
		}
	}
	return dm
}

// Close releases any open gRPC connections held by the DependencyManager.
func (dm *DependencyManager) Close() {
	if dm.privateConn != nil {
		dm.privateConn.Close()
	}
}

// AgentDependencyManager holds gRPC clients for agent CLI commands.
type AgentDependencyManager struct {
	AgentPrivateClient agent_grpc_private.AgentPrivateClient
	privateConn        *grpc.ClientConn
}

func AgentDependencyManagerBuilder(deps ...string) *AgentDependencyManager {
	dm := &AgentDependencyManager{}
	for _, dep := range deps {
		switch dep {
		case RequireAgentPrivateClient:
			conn, err := grpc.NewClient(
				fmt.Sprintf("unix://%s", agentSocketPath),
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			)
			if err != nil {
				panic("Failed to connect to agent: " + err.Error())
			}
			dm.privateConn = conn
			dm.AgentPrivateClient = agent_grpc_private.NewAgentPrivateClient(conn)
		}
	}
	return dm
}

// Close releases any open gRPC connections held by the AgentDependencyManager.
func (dm *AgentDependencyManager) Close() {
	if dm.privateConn != nil {
		dm.privateConn.Close()
	}
}
