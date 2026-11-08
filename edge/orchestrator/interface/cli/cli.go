package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands/cloud"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands/containers"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands/deploy"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands/nodes"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands/streams"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands/tokens"
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
	root.AddCommand(tokens.TokensCmd)
	registerDevCommands(root)
}

func init() {
	AddCommands(RootCmd)
}

func (o *OrchestratorCLI) Execute() {
	// A Docker-installed Orchestrator has its socket inside the container:
	// run the command there instead (see docker_delegate.go).
	if code, ok := delegateToContainer(os.Args[1:], realRunner()); ok {
		os.Exit(code)
	}
	RootCmd.Execute()
}
