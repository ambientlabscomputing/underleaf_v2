package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/repository"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// JoinCmd enrolls this machine into an existing cluster. It lives on the
// daemon binary (ufagentd) because that is what install.sh installs on a worker;
// `ufagentd join <string>` is therefore what install.sh's `join` subcommand runs.
var JoinCmd = &cobra.Command{
	Use:   "join <join-string>",
	Short: "Join this machine to a cluster using a string from `orcli nodes add`",
	Long: `Verifies the manager against the CA fingerprint inside the join string, then
sends the single-use secret and a certificate request, and stores the resulting
node certificate. Nothing is sent to a server that fails the fingerprint check.

Running it again with a fresh string re-enrolls the node and keeps its node id
(use this if the node's certificate expired while it was switched off).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true // a failed join is not a usage error
		force, _ := cmd.Flags().GetBool("force")
		cfg := utils.GetConfig(utils.AgentConfig)
		stateDir := utils.ClusterStateDir(cfg, "node")

		out, err := service.JoinCluster(cmd.Context(), args[0], service.JoinOptions{
			StateDir: stateDir,
			Force:    force,
			Host:     (&service.HostService{}).GetHostInfo,
			SaveNode: func(n *types.Node) error {
				repo, err := repository.NewRepository()
				if err != nil {
					return err
				}
				if err := repo.Start(); err != nil {
					return err
				}
				return repo.Node.CreateNode(n)
			},
		})
		if err != nil {
			return err
		}
		verb := "joined"
		if out.ReEnrolled {
			verb = "re-enrolled (node id kept)"
		}
		fmt.Printf("%s as node %s; certificate valid until %s\n", verb, out.NodeID, out.NotAfter.Format("2006-01-02"))
		if err := service.VerifyMembership(cmd.Context(), out.Member); err != nil {
			fmt.Fprintf(os.Stderr, "warning: credentials were issued but the mTLS check against the manager failed: %v\n", err)
			return fmt.Errorf("join incomplete")
		}
		fmt.Println("mTLS connection to the manager verified. Start (or restart) ufagentd to bring this node online.")
		return nil
	},
}

func init() {
	JoinCmd.Flags().Bool("force", false, "leave the cluster this node currently belongs to (if its certificate is still valid) and join a different one")
	RootCmd.AddCommand(JoinCmd)
}
