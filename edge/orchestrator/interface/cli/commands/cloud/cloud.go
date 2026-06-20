package cloud

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	grpc_private "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_private"
	cli_utils "github.com/ambientlabscomputing/underleaf_v2/shared/cli/utils"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// CloudCmd is the top-level "orcli cloud" command group.
var CloudCmd = &cobra.Command{
	Use:   "cloud",
	Short: "Manage cloud registration and connectivity",
}

var registerClusterName string
var registerClusterID string

var registerCmd = &cobra.Command{
	Use:   "register",
	Short: "Register this cluster with the Underleaf cloud",
	Run: func(cmd *cobra.Command, args []string) {
		dm := cli_utils.DependencyManagerBuilder(cli_utils.RequireOrchPrivateClient)
		defer dm.Close()

		// Auto-generate a cluster ID if not provided
		if registerClusterID == "" {
			registerClusterID = types.GenerateID("cluster")
		}
		if registerClusterName == "" {
			registerClusterName = registerClusterID
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := dm.OrchestratorPrivateClient.InitiateCloudRegistration(ctx, &grpc_private.InitiateCloudRegistrationRequest{
			ClusterName: registerClusterName,
			ClusterId:   registerClusterID,
		})
		if err != nil {
			fmt.Printf("Error initiating registration: %v\n", err)
			return
		}

		fmt.Println()
		fmt.Println("=== Underleaf Cloud Registration ===")
		fmt.Printf("Visit the following URL to complete registration:\n\n  %s\n\n", resp.VerificationUriComplete)
		fmt.Printf("Code: %s\n", resp.UserCode)
		fmt.Printf("Expires in: %d seconds\n\n", resp.ExpiresIn)
		fmt.Println("Waiting for approval in the browser...")
		fmt.Println()

		// Poll status until registered or expired
		ticker := time.NewTicker(time.Duration(resp.Interval) * time.Second)
		defer ticker.Stop()

		deadline := time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
		for range ticker.C {
			if time.Now().After(deadline) {
				fmt.Println("Registration timed out. Please try again.")
				return
			}

			statusCtx, statusCancel := context.WithTimeout(context.Background(), 5*time.Second)
			statusResp, err := dm.OrchestratorPrivateClient.GetCloudRegistrationStatus(statusCtx, &grpc_private.GetCloudRegistrationStatusRequest{})
			statusCancel()

			if err != nil {
				continue
			}

			switch statusResp.Status {
			case "registered":
				fmt.Printf("✓ Cluster registered successfully! (ID: %s)\n", statusResp.ClusterId)
				return
			case "expired":
				fmt.Println("Registration expired. Please run `orcli cloud register` again.")
				return
			case "failed":
				fmt.Println("Registration failed. Check logs for details.")
				return
			default:
				fmt.Print(".")
			}
		}
	},
}

func init() {
	registerCmd.Flags().StringVarP(&registerClusterName, "name", "n", "", "Human-readable name for the cluster")
	registerCmd.Flags().StringVar(&registerClusterID, "id", "", "Stable cluster ID (auto-generated if not provided)")
	CloudCmd.AddCommand(registerCmd)
}
