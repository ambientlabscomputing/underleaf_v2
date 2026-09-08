package cli

import (
	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands/cloud"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands/containers"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands/deploy"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands/nodes"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands/streams"
)

type OrchestratorCLI struct{}

var RootCmd = &cobra.Command{
	Use:   "orcli",
	Short: "Orchestrator CLI for managing the edge orchestrator",
}

// AddCommands mounts every orchestrator command onto root. Exposed
// separately from init() so the ufctl gateway binary can mount the exact
// same command implementations onto its own root without duplicating any
// command logic — orcli itself just calls this on its own RootCmd below.
func AddCommands(root *cobra.Command) {
	root.AddCommand(commands.RunCmd)
	root.AddCommand(nodes.NodesCmd)
	root.AddCommand(streams.StreamsCmd)
	root.AddCommand(containers.ContainersCmd)
	root.AddCommand(cloud.CloudCmd)
	root.AddCommand(deploy.DeployCmd)
	registerDevCommands(root)
}

func init() {
	AddCommands(RootCmd)
}

func (o *OrchestratorCLI) Execute() {
	RootCmd.Execute()
}
