package nodes

import (
	"context"
	"fmt"

	grpc_private "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_private"
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/utils"
	"github.com/spf13/cobra"
)

var LsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List all nodes in the edge orchestrator",
	Run: func(cmd *cobra.Command, args []string) {
		dep_mgr := utils.DependencyManagerBuilder(utils.RequireOrchPrivateClient)
		defer dep_mgr.Close()

		resp, err := dep_mgr.OrchestratorPrivateClient.GetNodes(context.Background(), &grpc_private.GetNodesRequest{})
		if err != nil {
			fmt.Printf("Error fetching nodes: %v\n", err)
			return
		}
		nodes := resp.GetResults()
		if len(nodes) == 0 {
			fmt.Println("No nodes found in the edge orchestrator.")
			return
		}
		fmt.Println("Nodes in the edge orchestrator:")
		for _, node := range nodes {
			fmt.Printf("- ID: %s, Name: %s\n", node.GetId(), node.GetName())
		}
	},
}
