package start

import "github.com/spf13/cobra"

var StartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the edge agent",
	// Run: func(cmd *cobra.Command, args []string) {
	// 	// Implement the start logic here
	// },
}
