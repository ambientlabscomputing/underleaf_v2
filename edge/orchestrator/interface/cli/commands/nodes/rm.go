package nodes

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	grpc_private "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_private"
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/ui"
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/utils"
)

var RmCmd = &cobra.Command{
	Use:   "rm <node-id>",
	Short: "Remove a node and revoke its certificates",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dep := utils.DependencyManagerBuilder(utils.RequireOrchPrivateClient)
		defer dep.Close()

		resp, err := dep.ClusterPrivateClient.RemoveNode(cmd.Context(), &grpc_private.RemoveNodeRequest{NodeId: args[0]})
		if err != nil {
			ui.PrintError("Could not remove node: %v", err)
			os.Exit(1)
		}
		if n := resp.GetRevokedCertificates(); n > 0 {
			fmt.Printf("Removed node %s and revoked %d certificate(s). It can no longer connect; its live link (if any) was dropped.\n", args[0], n)
		} else {
			fmt.Printf("Removed node %s (it had no certificate to revoke).\n", args[0])
		}
	},
}
