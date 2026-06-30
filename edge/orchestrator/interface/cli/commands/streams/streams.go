package streams

import (
	"github.com/spf13/cobra"
)

var StreamsCmd = &cobra.Command{
	Use:   "streams",
	Short: "Manage streams in the edge orchestrator",
	// Run: func(cmd *cobra.Command, args []string) {
	// 	// Implementation of the nodes command logic goes here
	// },
}

func init() {
	StreamsCmd.AddCommand(LsCmd)
	StreamsCmd.AddCommand(NewCmd)
}
