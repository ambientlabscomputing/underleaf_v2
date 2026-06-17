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

func init() {
	RootCmd.AddCommand(start.StartCmd)
	RootCmd.AddCommand(ping.PingCmd)
	RootCmd.AddCommand(register.RegisterCmd)
	RootCmd.AddCommand(containers.ContainersCmd)
}

func (o *AgentCLI) Execute() {
	RootCmd.Execute()
}
