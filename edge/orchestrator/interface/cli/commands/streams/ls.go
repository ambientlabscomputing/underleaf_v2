package streams

import (
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/utils"
	"github.com/spf13/cobra"
)

var LsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List connections in the edge orchestrator",
	Run:   LsRun,
}

func init() {
	// Search queries
	LsCmd.Flags().StringP("name", "n", "", "Filter connections by name")
	LsCmd.Flags().StringP("node-id", "i", "", "Filter connections by node ID")
	LsCmd.Flags().StringP("state", "s", "", "Filter connections by state")
	LsCmd.Flags().StringP("status", "t", "", "Filter connections by status")
}

func LsRun(cmd *cobra.Command, args []string) {
	dep_mgr := utils.DependencyManagerBuilder(utils.RequireOrchPrivateClient)
	defer dep_mgr.Close()

	// TODO: need a new service method in the orch to call the API
}
