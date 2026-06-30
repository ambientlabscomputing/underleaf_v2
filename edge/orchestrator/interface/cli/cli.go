package cli

import (
	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands/cloud"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands/containers"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands/nodes"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands/streams"
)

type OrchestratorCLI struct{}

var RootCmd = &cobra.Command{
	Use:   "orcli",
	Short: "Orchestrator CLI for managing the edge orchestrator",
}

func init() {
	RootCmd.AddCommand(commands.RunCmd)
	RootCmd.AddCommand(nodes.NodesCmd)
	RootCmd.AddCommand(streams.StreamsCmd)
	RootCmd.AddCommand(containers.ContainersCmd)
	RootCmd.AddCommand(cloud.CloudCmd)
	registerDevCommands(RootCmd)
}

func (o *OrchestratorCLI) Execute() {
	RootCmd.Execute()
}
