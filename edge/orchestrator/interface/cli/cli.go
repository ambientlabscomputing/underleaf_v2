package cli

import (
	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands/nodes"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands/run"
)

type OrchestratorCLI struct{}

var RootCmd = &cobra.Command{
	Use:   "orctl",
	Short: "Orchestrator CLI for managing the edge orchestrator",
}

func init() {
	RootCmd.AddCommand(run.RunCmd)
	RootCmd.AddCommand(nodes.NodesCmd)
}

func (o *OrchestratorCLI) Execute() {
	RootCmd.Execute()
}
