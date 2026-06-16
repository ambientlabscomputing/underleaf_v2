package cli

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/cli/commands/start"
	"github.com/spf13/cobra"
)

type AgentCLI struct{}

var RootCmd = &cobra.Command{
	Use:   "ufagent",
	Short: "Agent CLI for managing the edge agent",
}

func init() {
	// Add subcommands to RootCmd here, e.g.:
	RootCmd.AddCommand(start.StartCmd)
}

func (o *AgentCLI) Execute() {
	RootCmd.Execute()
}
