package ping

import (
	"context"
	"fmt"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/cli/utils"
	"github.com/spf13/cobra"
)

var PingCmd = &cobra.Command{
	Use:   "ping",
	Short: "Ping the agent daemon and report connectivity to the orchestrator",
	Run: func(cmd *cobra.Command, args []string) {
		dep := utils.AgentDependencyManagerBuilder(utils.RequireAgentPrivateClient)
		defer dep.Close()

		sentAt := time.Now()
		resp, err := dep.AgentPrivateClient.Ping(context.Background(), nil)
		if err != nil {
			fmt.Printf("✗ agent unreachable: %v\n", err)
			return
		}
		rtt := time.Since(sentAt).Milliseconds()

		// Agent self-ping result
		fmt.Printf("agent    ✓ status=%s  rtt=%dms\n", resp.GetAgentStatus(), rtt)

		// Orchestrator reachability (triggered by the daemon)
		if resp.GetOrchestratorReachable() {
			orchRTT := time.Now().UnixMilli() - resp.GetOrchestratorTimestampMs()
			fmt.Printf("orchestrator ✓ responder=%s  rtt≈%dms\n", resp.GetOrchestratorResponder(), orchRTT)
		} else {
			fmt.Printf("orchestrator ✗ unreachable: %s\n", resp.GetOrchestratorError())
		}
	},
}
