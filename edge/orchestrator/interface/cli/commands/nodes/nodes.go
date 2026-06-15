package nodes

import "github.com/spf13/cobra"

var NodesCmd = &cobra.Command{
	Use:   "nodes",
	Short: "Manage nodes in the edge orchestrator",
	// Run: func(cmd *cobra.Command, args []string) {
	// 	// Implementation of the nodes command logic goes here
	// },
}

func init() {
	NodesCmd.AddCommand(LsCmd)
}
