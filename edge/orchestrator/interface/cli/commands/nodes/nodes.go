package nodes

import "github.com/spf13/cobra"

var NodesCmd = &cobra.Command{
	Use:   "nodes",
	Short: "Manage nodes in the edge orchestrator",
}

func init() {
	NodesCmd.AddCommand(LsCmd)
	NodesCmd.AddCommand(AddCmd)
	NodesCmd.AddCommand(RmCmd)
}
