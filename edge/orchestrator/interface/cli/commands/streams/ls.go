package streams

import (
	"io"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/ui"
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
	LsCmd.Flags().StringP("connection-id", "c", "", "Filter streams by connection ID")
	LsCmd.Flags().StringP("state", "s", "", "Filter streams by state")
}

func LsRun(cmd *cobra.Command, args []string) {
	dep_mgr := utils.DependencyManagerBuilder(utils.RequireOrchPrivateClient)
	defer dep_mgr.Close()

	connIDFilter, _ := cmd.Flags().GetString("connection-id")
	stateFilter, _ := cmd.Flags().GetString("state")

	stream, err := dep_mgr.OrchestratorPrivateClient.ListStreams(cmd.Context(), &emptypb.Empty{})
	if err != nil {
		ui.PrintError("Failed to list streams: %v", err)
		return
	}

	count := 0
	for {
		streamState, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			ui.PrintError("Failed to list streams: %v", err)
			return
		}
		if connIDFilter != "" && streamState.ConnectionId != connIDFilter {
			continue
		}
		if stateFilter != "" && streamState.State != stateFilter {
			continue
		}
		ui.Printf("StreamID=%s Type=%s ConnectionID=%s State=%s\n",
			streamState.StreamId, streamState.Type, streamState.ConnectionId, streamState.State)
		count++
	}
	if count == 0 {
		ui.Printf("No streams found\n")
	}
}
