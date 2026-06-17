package cli

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/cli/commands/ping"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/cli/commands/start"
	"github.com/spf13/cobra"
)

type AgentCLI struct{}

var RootCmd = &cobra.Command{
	Use:   "ufagent",
	Short: "Agent CLI for managing the edge agent",
}

func init() {
	RootCmd.AddCommand(start.StartCmd)
	RootCmd.AddCommand(ping.PingCmd)
}

func (o *AgentCLI) Execute() {
	RootCmd.Execute()
}
