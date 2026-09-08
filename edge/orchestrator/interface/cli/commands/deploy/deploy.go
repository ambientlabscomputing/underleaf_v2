package deploy

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_private"
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/ui"
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/utils"
)

const (
	pollInterval = 2 * time.Second
	pollTimeout  = 5 * time.Minute
)

var DeployCmd = &cobra.Command{
	Use:   "deploy <source>",
	Short: "Deploy an application from a git repository manifest",
	Long: `Resolves .underleaf/deploy.yaml from a source repo, persists it as a tracked
deployment, and reconciles it onto the agent node.

Examples:
  deploy gh:ambientlabscomputing/n8n
  deploy gh:ambientlabscomputing/n8n --ref develop
  deploy gh:myorg/private-repo --token $GITHUB_TOKEN`,
	Args: cobra.ExactArgs(1),
	Run:  DeployRun,
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List tracked deployments",
	Run:   ListRun,
}

func init() {
	DeployCmd.Flags().String("ref", "", "git ref to deploy (branch, tag, or SHA); overrides any @ref in the source")
	DeployCmd.Flags().String("token", "", "GitHub token, for private repos or to avoid rate limits")
	DeployCmd.AddCommand(listCmd)
}

func DeployRun(cmd *cobra.Command, args []string) {
	dep := utils.DependencyManagerBuilder(utils.RequireOrchPrivateClient)
	defer dep.Close()

	ref, _ := cmd.Flags().GetString("ref")
	token, _ := cmd.Flags().GetString("token")

	resp, err := dep.OrchestratorPrivateClient.Deploy(context.Background(), &grpc_private.DeployRequest{
		Source: args[0],
		Ref:    ref,
		Token:  token,
	})
	if err != nil {
		ui.PrintError("deploy failed: %v", err)
		return
	}

	d := resp.GetDeployment()
	ui.Printf("deployment created: id=%s name=%s repo=%s ref=%s\n", d.GetId(), d.GetSpec().GetName(), d.GetRepo(), d.GetRef())
	fmt.Print("reconciling")

	// Reconcile runs in the background on the orchestrator — poll for the
	// outcome instead of blocking on one long RPC (mirrors `orcli cloud
	// register`'s poll loop).
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	deadline := time.Now().Add(pollTimeout)
	for range ticker.C {
		if time.Now().After(deadline) {
			fmt.Println()
			ui.PrintError("timed out waiting for reconcile — check `orcli deploy list` for status")
			return
		}

		statusCtx, statusCancel := context.WithTimeout(context.Background(), 5*time.Second)
		latest, err := dep.OrchestratorPrivateClient.GetDeployment(statusCtx, &grpc_private.GetDeploymentRequest{Id: d.GetId()})
		statusCancel()
		if err != nil {
			continue
		}

		switch latest.GetStatus() {
		case "succeeded":
			fmt.Println()
			ui.Printf("reconciled: %d service(s) applied\n", len(latest.GetSpec().GetServices()))
			return
		case "failed":
			fmt.Println()
			ui.PrintError("reconcile failed: %s", latest.GetError())
			return
		default:
			fmt.Print(".")
		}
	}
}

func ListRun(cmd *cobra.Command, args []string) {
	dep := utils.DependencyManagerBuilder(utils.RequireOrchPrivateClient)
	defer dep.Close()

	resp, err := dep.OrchestratorPrivateClient.ListDeployments(context.Background(), &grpc_private.ListDeploymentsRequest{})
	if err != nil {
		ui.PrintError("list failed: %v", err)
		return
	}

	deployments := resp.GetDeployments()
	if len(deployments) == 0 {
		fmt.Println("no deployments found — run `orcli deploy <source>` first")
		return
	}
	fmt.Printf("%-28s  %-20s  %-32s  %-10s  %s\n", "ID", "NAME", "REPO", "REF", "STATUS")
	for _, d := range deployments {
		fmt.Printf("%-28s  %-20s  %-32s  %-10s  %s\n", d.GetId(), d.GetSpec().GetName(), d.GetRepo(), d.GetRef(), d.GetStatus())
	}
}
