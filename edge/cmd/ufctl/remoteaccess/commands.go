package remoteaccess

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
)

// AddCommands mounts login, logout and connect on root.
func AddCommands(root *cobra.Command) {
	var cloudURL string
	login := &cobra.Command{
		Use:   "login",
		Short: "Sign in to the cloud and store a device key (for `ufctl connect`)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cloudURL == "" {
				cloudURL = os.Getenv("UNDERLEAF_CLOUD_URL")
			}
			return Login(cmd.Context(), LoginOptions{CloudURL: cloudURL, Out: cmd.OutOrStdout()})
		},
	}
	login.Flags().StringVar(&cloudURL, "cloud-url", "", "cloud API base URL (default "+DefaultCloudURL+")")

	logout := &cobra.Command{
		Use:   "logout",
		Short: "Forget the stored device key on this machine",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := Delete(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Logged out. Revoke the device in your account to invalidate the key itself.")
			return nil
		},
	}

	var port int
	connect := &cobra.Command{
		Use:   "connect <cluster-id>",
		Short: "Open a local proxy to a cluster's Orchestrator UI and API through the cloud (owner only)",
		Long: "Exposes http://localhost:<port> and forwards it to the cluster's Orchestrator through the cloud.\n" +
			"For you, on this machine, with your own account. It cannot be shared and listens on loopback only.\n" +
			"If the cloud is down, SSH to the machine instead.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cred, err := Load()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			return Connect(ctx, ConnectOptions{Cred: *cred, ClusterID: args[0], Port: port, Out: cmd.OutOrStdout()})
		},
	}
	connect.Flags().IntVar(&port, "port", 0, "local port (default: a free one)")

	root.AddCommand(login, logout, connect)
}
