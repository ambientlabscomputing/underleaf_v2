package containers

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/cli/utils"
)

var ContainersCmd = &cobra.Command{
	Use:   "containers",
	Short: "Manage Docker containers on the local node",
}

var ingestCmd = &cobra.Command{
	Use:   "ingest",
	Short: "Ingest local Docker containers and sync them to the orchestrator",
	Run: func(cmd *cobra.Command, args []string) {
		dep := utils.AgentDependencyManagerBuilder(utils.RequireAgentPrivateClient)
		defer dep.Close()

		resp, err := dep.AgentPrivateClient.IngestContainers(context.Background(), nil)
		if err != nil {
			fmt.Printf("✗ ingest failed: %v\n", err)
			return
		}

		fmt.Printf("ingested %d container(s)\n", len(resp.GetContainers()))
		for _, c := range resp.GetContainers() {
			fmt.Printf("  docker_id=%.12s image=%s status=%s\n", c.GetDockerId(), c.GetImage(), c.GetStatus())
		}
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List containers stored in the agent repository",
	Run: func(cmd *cobra.Command, args []string) {
		dep := utils.AgentDependencyManagerBuilder(utils.RequireAgentPrivateClient)
		defer dep.Close()

		resp, err := dep.AgentPrivateClient.ListContainers(context.Background(), nil)
		if err != nil {
			fmt.Printf("✗ list failed: %v\n", err)
			return
		}

		containers := resp.GetContainers()
		if len(containers) == 0 {
			fmt.Println("no containers found — run `ufagent containers ingest` first")
			return
		}
		fmt.Printf("%-14s  %-40s  %s\n", "DOCKER_ID", "IMAGE", "STATUS")
		for _, c := range containers {
			fmt.Printf("%.12s    %-40s  %s\n", c.GetDockerId(), c.GetImage(), c.GetStatus())
		}
	},
}

func init() {
	ContainersCmd.AddCommand(ingestCmd)
	ContainersCmd.AddCommand(listCmd)
}
