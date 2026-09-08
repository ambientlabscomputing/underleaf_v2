package cli

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/cli/commands/containers"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/cli/commands/ping"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/cli/commands/register"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/cli/commands/start"
	"github.com/spf13/cobra"
)

type AgentCLI struct{}

var RootCmd = &cobra.Command{
	Use:   "ufagent",
	Short: "Agent CLI for managing the edge agent",
}

// AddCommands mounts every agent command onto root. Exposed separately from
// init() so the ufctl gateway binary can mount the exact same command
// implementations onto its own root without duplicating any command logic
// — ufagent itself just calls this on its own RootCmd below.
func AddCommands(root *cobra.Command) {
	root.AddCommand(start.StartCmd)
	root.AddCommand(ping.PingCmd)
	root.AddCommand(register.RegisterCmd)
	root.AddCommand(containers.ContainersCmd)
}

func init() {
	AddCommands(RootCmd)
}

func (o *AgentCLI) Execute() {
	RootCmd.Execute()
}
