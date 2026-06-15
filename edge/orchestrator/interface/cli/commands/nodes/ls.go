package nodes

import (
	"fmt"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/utils"
	"github.com/spf13/cobra"
)

var LsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List all nodes in the edge orchestrator",
	Run: func(cmd *cobra.Command, args []string) {
		dep_mgr := utils.DependencyManagerBuilder(utils.RequireNodeService)
		node_service := dep_mgr.NodeService
		nodes, err := node_service.GetNodes()
		if err != nil {
			fmt.Printf("Error fetching nodes: %v\n", err)
			return
		}
		if len(nodes) == 0 {
			fmt.Println("No nodes found in the edge orchestrator.")
			return
		}
		fmt.Println("Nodes in the edge orchestrator:")
		for _, node := range nodes {
			fmt.Printf("- ID: %s, Name: %s\n", node.ID, node.Name)
		}
	},
}
