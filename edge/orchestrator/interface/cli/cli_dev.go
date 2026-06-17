//go:build devmode

package cli

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cli/commands"
	"github.com/spf13/cobra"
)

func registerDevCommands(root *cobra.Command) {
	root.AddCommand(commands.SqlCmd)
}
