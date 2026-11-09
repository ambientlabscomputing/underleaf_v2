package nodes

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	grpc_private "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_private"
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/ui"
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/utils"
)

var LsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List all nodes with their link state and certificate expiry",
	Run: func(cmd *cobra.Command, args []string) {
		dep := utils.DependencyManagerBuilder(utils.RequireOrchPrivateClient)
		defer dep.Close()

		resp, err := dep.ClusterPrivateClient.ListClusterNodes(cmd.Context(), &grpc_private.ListClusterNodesRequest{})
		if err != nil {
			ui.PrintError("Error fetching nodes: %v", err)
			os.Exit(1)
		}
		nodes := resp.GetNodes()
		if len(nodes) == 0 {
			fmt.Println("No nodes found in the edge orchestrator.")
			return
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tTRANSPORT\tLINK\tCERT EXPIRES\t")
		var warned []string
		for _, n := range nodes {
			link := "down"
			if n.GetLinked() {
				link = "up"
			}
			expiry, warn := certColumn(n, time.Now())
			if warn != "" {
				warned = append(warned, fmt.Sprintf("%s (%s): %s", n.GetName(), n.GetId(), warn))
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t\n", n.GetId(), n.GetName(), n.GetTransport(), link, expiry)
		}
		w.Flush()
		for _, m := range warned {
			ui.PrintError("warning: certificate %s", m)
		}
		if len(warned) > 0 {
			fmt.Println("A node renews on its own once it is within 90 days of expiry while it can reach the manager; a node whose certificate has expired must re-join with a fresh `orcli nodes add` string (it keeps its node id).")
		}
	},
}

// certColumn renders the expiry cell and, when the certificate is within 30
// days of expiry (or past it), a warning message.
func certColumn(n *grpc_private.ClusterNode, now time.Time) (cell, warn string) {
	if n.GetCertNotAfterUnix() == 0 {
		return "-", ""
	}
	na := time.Unix(n.GetCertNotAfterUnix(), 0)
	days := int(na.Sub(now).Hours() / 24)
	cell = fmt.Sprintf("%s (%dd)", na.Format("2006-01-02"), days)
	switch {
	case n.GetCertExpired():
		return na.Format("2006-01-02") + " EXPIRED", "has expired"
	case n.GetCertExpiring():
		return cell + " !", fmt.Sprintf("expires in %d day(s)", days)
	}
	return cell, ""
}
