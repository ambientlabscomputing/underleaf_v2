package nodes

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	grpc_private "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_private"
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/ui"
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/utils"
)

// EnvInstallURL lets a deployment print its own install.sh location.
const EnvInstallURL = "UNDERLEAF_INSTALL_URL"

var AddCmd = &cobra.Command{
	Use:   "add",
	Short: "Mint a join string for a new node (first run also enables the cluster listener)",
	Long: `Prints a single-use join string (valid 15 minutes) for one new node.

The string carries the manager's address, the fingerprint of the cluster CA and a
one-time secret. The new node verifies the manager against the fingerprint before
it sends the secret, then receives its own certificate. The first run also starts
the cluster listener, which accepts mTLS connections only.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		advertise, _ := cmd.Flags().GetString("advertise")
		dep := utils.DependencyManagerBuilder(utils.RequireOrchPrivateClient)
		defer dep.Close()

		resp, err := dep.ClusterPrivateClient.AddNode(cmd.Context(), &grpc_private.AddNodeRequest{Advertise: advertise})
		if err != nil {
			ui.PrintError("Could not create a join string: %v", err)
			os.Exit(1)
		}
		installURL := os.Getenv(EnvInstallURL)
		if installURL == "" {
			installURL = "https://<your-install-host>/install.sh"
		}
		expires := time.Unix(resp.GetExpiresAtUnix(), 0)
		fmt.Printf("Join string (single use, expires %s, in %s):\n\n", expires.Format("15:04:05"), time.Until(expires).Round(time.Minute))
		fmt.Printf("  %s\n\n", resp.GetJoinString())
		fmt.Println("On the new machine run either of:")
		fmt.Printf("  curl -fsSL %s | sh -s -- join %s\n", installURL, resp.GetJoinString())
		fmt.Printf("  ufagentd join %s        (if ufagentd is already installed there)\n\n", resp.GetJoinString())
		fmt.Printf("Manager address : %s\n", resp.GetAdvertise())
		fmt.Printf("CA fingerprint  : %s\n", resp.GetCaFingerprint())
		fmt.Println("The cluster listener accepts mTLS connections only; the new machine must be able to reach the address above.")
	},
}

func init() {
	AddCmd.Flags().String("advertise", "", "host[:port] the new node should dial (default: this machine's primary address and the cluster port)")
}
