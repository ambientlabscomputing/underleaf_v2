package register

import (
	"context"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/utils"
	"github.com/spf13/cobra"
)

var RegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register the agent node with the orchestrator",
	Run: func(cmd *cobra.Command, args []string) {
		dep := utils.AgentDependencyManagerBuilder(utils.RequireAgentPrivateClient)
		defer dep.Close()

		resp, err := dep.AgentPrivateClient.Register(context.Background(), nil)
		if err != nil {
			fmt.Printf("✗ registration failed: %v\n", err)
			return
		}

		node := resp.GetNode()

		// print resp.status, node.name and node.id
		fmt.Printf("registration successful: node.name=%s node.id=%s\n", node.GetName(), node.GetId())
	},
}
