package utils

import (
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	grpc_private "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_private"
)

const (
	RequirePrivateClient = "RequirePrivateClient"
)

const privateSocketPath = "/tmp/undf-orch.sock"

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
