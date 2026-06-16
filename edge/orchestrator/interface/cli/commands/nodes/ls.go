package nodes

import (
	"context"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/cli/utils"
	"github.com/spf13/cobra"
)

var LsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List all nodes in the edge orchestrator",
	Run: func(cmd *cobra.Command, args []string) {
		dep_mgr := utils.DependencyManagerBuilder(utils.RequirePrivateClient)
		defer dep_mgr.Close()

		resp, err := dep_mgr.OrchestratorPrivateClient.GetNodes(context.Background(), nil)
		if err != nil {
			fmt.Printf("Error fetching nodes: %v\n", err)
			return
		}
		nodes := resp.GetNodes()
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
