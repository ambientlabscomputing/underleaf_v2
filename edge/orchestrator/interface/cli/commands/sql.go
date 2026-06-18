//go:build devmode

package commands

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_private"
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/ui"
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/utils"
	"github.com/spf13/cobra"
)

var SqlCmd = &cobra.Command{
	Use:   "sql",
	Short: "Execute an SQL query on the edge orchestrator",
	Run: func(cmd *cobra.Command, args []string) {
		dep_mgr := utils.DependencyManagerBuilder(utils.RequireOrchPrivateClient)
		defer dep_mgr.Close()

		query, _ := cmd.Flags().GetString("query")
		_ = query

		ui.Debug("Executing SQL query: %s", query)
		resp, err := dep_mgr.OrchestratorPrivateClient.SqlQuery(cmd.Context(), &grpc_private.SqlQueryRequest{Query: query})
		if err != nil {
			ui.PrintError("SQL query failed: %v", err)
			return
		}
		ui.Printf("Query result:\n%s", resp.GetResult())
	},
}

func init() {
	SqlCmd.Flags().StringP("query", "q", "", "SQL query to execute")
	SqlCmd.MarkFlagRequired("query")
}
