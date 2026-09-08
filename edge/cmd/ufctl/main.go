// ufctl is the unified, user-facing Underleaf CLI. It carries no command
// logic of its own — it's a thin gateway that mounts orcli's (orchestrator)
// and ufagent's (agent) command packages onto one root, so a user only has
// to remember one command name. orcli and ufagent remain the real,
// independently developed and tested binaries; this just avoids making
// users choose between them for day-to-day use.
//
// Orchestrator/fleet commands (deploy, containers, nodes, streams, cloud)
// live at the top level. Agent-local commands (ping, start, register, and
// the agent's own node-local containers view) live under `ufctl agent ...`
// — both sides happen to define a `containers` command with different
// scopes (fleet-wide vs. this node's Docker daemon), so they can't both sit
// at the top level without colliding.
package main

import (
	"github.com/spf13/cobra"

	agentcli "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/cli"
	orchestratorcli "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli"
)

var rootCmd = &cobra.Command{
	Use:   "ufctl",
	Short: "Underleaf CLI",
}

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage the local agent node (ping, start, register, local containers)",
}

func init() {
	orchestratorcli.AddCommands(rootCmd)
	agentcli.AddCommands(agentCmd)
	rootCmd.AddCommand(agentCmd)
}

func main() {
	rootCmd.Execute()
}
