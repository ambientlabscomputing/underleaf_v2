package streams

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_private"
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/ui"
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/utils"
	"github.com/spf13/cobra"
)

var NewCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a new stream via the edge orchestrator",
	Run:   NewRun,
}

func init() {
	// Search queries
	NewCmd.Flags().StringP("node-id", "i", "", "target Node for the new connection")
	NewCmd.Flags().StringP("type", "t", "", "target type for the new connection")
	NewCmd.Flags().StringP("port", "p", "", "target port for the new connection")
}

func NewRun(cmd *cobra.Command, args []string) {
	dep_mgr := utils.DependencyManagerBuilder(utils.RequireOrchPrivateClient)
	defer dep_mgr.Close()

	nodeID, _ := cmd.Flags().GetString("node-id")
	type_, _ := cmd.Flags().GetString("type")
	port, _ := cmd.Flags().GetInt("port")

	stream, err := dep_mgr.OrchestratorPrivateClient.NewStream(cmd.Context(), &grpc_private.NewStreamRequest{
		NodeId: nodeID,
		Type:   type_,
		Port:   int32(port),
	})
	if err != nil {
		ui.PrintError("Failed to create new stream: %v", err)
		return
	}

	ui.Printf("New stream created: ID=%s, Type=%s, ConnectionID=%s\n", stream.Id, stream.Type, stream.ConnectionId)
}
