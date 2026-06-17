package containers

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/cli/utils"
)

var ContainersCmd = &cobra.Command{
	Use:   "containers",
	Short: "Manage Docker containers across the fleet",
}

var ingestCmd = &cobra.Command{
	Use:   "ingest",
	Short: "Trigger the local agent to ingest its Docker containers",
	Run: func(cmd *cobra.Command, args []string) {
		dep := utils.DependencyManagerBuilder(utils.RequireOrchPrivateClient)
		defer dep.Close()

		resp, err := dep.OrchestratorPrivateClient.TriggerIngest(context.Background(), nil)
		if err != nil {
			fmt.Printf("✗ ingest trigger failed: %v\n", err)
			return
		}
		fmt.Printf("ingest complete: %d container(s) synced\n", resp.GetContainerCount())
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List containers stored in the orchestrator",
	Run: func(cmd *cobra.Command, args []string) {
		dep := utils.DependencyManagerBuilder(utils.RequireOrchPrivateClient)
		defer dep.Close()

		resp, err := dep.OrchestratorPrivateClient.ListContainers(context.Background(), nil)
		if err != nil {
			fmt.Printf("✗ list failed: %v\n", err)
			return
		}
		containers := resp.GetContainers()
		if len(containers) == 0 {
			fmt.Println("no containers found — run `orctl containers ingest` first")
			return
		}
		fmt.Printf("%-14s  %-14s  %-40s  %s\n", "DOCKER_ID", "NODE_ID", "IMAGE", "STATUS")
		for _, c := range containers {
			fmt.Printf("%.12s    %.12s    %-40s  %s\n",
				c.GetDockerId(), c.GetNodeId(), c.GetImage(), c.GetStatus())
		}
	},
}

func init() {
	ContainersCmd.AddCommand(ingestCmd)
	ContainersCmd.AddCommand(listCmd)
}
