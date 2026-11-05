// Package tokens implements `orcli tokens`: minting, listing and revoking
// scoped API tokens over the owner-only unix socket (RFD 5, step 6).
package tokens

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_private"
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/ui"
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/utils"
)

var TokensCmd = &cobra.Command{
	Use:   "tokens",
	Short: "Manage scoped API tokens for the Orchestrator REST API",
	Long: `Tokens are bearer credentials for the REST API (Authorization: Bearer <token>).
They are created over the local unix socket, so only the machine's owner can
mint them, are stored hashed, always expire, and can be revoked at any time.

Scopes (each includes the ones before it):
  read    read-only calls (list nodes, containers, deployments, logs)
  deploy  also deploy and run workloads. NOTE: deploying runs Docker on the
          host, which is root-equivalent; treat deploy tokens accordingly.
  admin   everything`,
}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a token (the secret is shown once)",
	Args:  cobra.NoArgs,
	Run:   createRun,
}

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List tokens (secrets are never shown)",
	Args:  cobra.NoArgs,
	Run:   lsRun,
}

var revokeCmd = &cobra.Command{
	Use:   "revoke <id>",
	Short: "Revoke a token immediately",
	Args:  cobra.ExactArgs(1),
	Run:   revokeRun,
}

func init() {
	createCmd.Flags().String("scope", "", "read | deploy | admin (required)")
	createCmd.Flags().String("ttl", "30d", "lifetime, e.g. 90m, 12h, 30d (1m to 365d)")
	createCmd.Flags().String("name", "", "label to recognise the token by")
	_ = createCmd.MarkFlagRequired("scope")
	TokensCmd.AddCommand(createCmd, lsCmd, revokeCmd)
}

// ParseTTL parses durations like "90m", "12h", "30d" or "2w".
func ParseTTL(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid ttl %q: want a number and a unit, e.g. 30d", s)
	}
	unit := map[byte]time.Duration{
		's': time.Second, 'm': time.Minute, 'h': time.Hour,
		'd': 24 * time.Hour, 'w': 7 * 24 * time.Hour,
	}[s[len(s)-1]]
	if unit == 0 {
		return 0, fmt.Errorf("invalid ttl %q: unit must be one of s, m, h, d, w", s)
	}
	n, err := strconv.ParseInt(s[:len(s)-1], 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid ttl %q: want a positive whole number and a unit, e.g. 30d", s)
	}
	if n > int64(1<<62)/int64(unit) {
		return 0, fmt.Errorf("invalid ttl %q: too large", s)
	}
	return time.Duration(n) * unit, nil
}

func createRun(cmd *cobra.Command, _ []string) {
	scope, _ := cmd.Flags().GetString("scope")
	ttlStr, _ := cmd.Flags().GetString("ttl")
	name, _ := cmd.Flags().GetString("name")
	ttl, err := ParseTTL(ttlStr)
	if err != nil {
		ui.PrintError("%v", err)
		return
	}

	dep := utils.DependencyManagerBuilder(utils.RequireOrchPrivateClient)
	defer dep.Close()
	resp, err := dep.OrchestratorTokensClient.CreateToken(context.Background(), &grpc_private.CreateTokenRequest{
		Name: name, Scope: scope, TtlSeconds: int64(ttl / time.Second),
	})
	if err != nil {
		ui.PrintError("create token failed: %v", err)
		return
	}
	info := resp.GetInfo()
	ui.Printf("Token created: id=%s scope=%s expires=%s\n", info.GetId(), info.GetScope(), time.Unix(info.GetExpiresAtUnix(), 0).Format(time.RFC3339))
	ui.Print("")
	ui.Print(resp.GetToken())
	ui.Print("")
	ui.Print("Copy it now: it is not stored and cannot be shown again. Use it as 'Authorization: Bearer <token>'.")
	if info.GetScope() != "read" {
		ui.Print("Note: this scope can run Docker workloads on the host, which is root-equivalent.")
	}
}

func lsRun(_ *cobra.Command, _ []string) {
	dep := utils.DependencyManagerBuilder(utils.RequireOrchPrivateClient)
	defer dep.Close()
	resp, err := dep.OrchestratorTokensClient.ListTokens(context.Background(), &grpc_private.ListTokensRequest{})
	if err != nil {
		ui.PrintError("list tokens failed: %v", err)
		return
	}
	if len(resp.GetTokens()) == 0 {
		ui.Print("No API tokens.")
		return
	}
	for _, t := range resp.GetTokens() {
		last := "never"
		if t.GetLastUsedAtUnix() != 0 {
			last = time.Unix(t.GetLastUsedAtUnix(), 0).Format(time.RFC3339)
		}
		name := t.GetName()
		if name == "" {
			name = "-"
		}
		ui.Printf("%s  %-7s %-8s expires %s  last used %s  name %s\n",
			t.GetId(), t.GetScope(), t.GetStatus(), time.Unix(t.GetExpiresAtUnix(), 0).Format(time.RFC3339), last, name)
	}
}

func revokeRun(_ *cobra.Command, args []string) {
	dep := utils.DependencyManagerBuilder(utils.RequireOrchPrivateClient)
	defer dep.Close()
	if _, err := dep.OrchestratorTokensClient.RevokeToken(context.Background(), &grpc_private.RevokeTokenRequest{Id: args[0]}); err != nil {
		ui.PrintError("revoke failed: %v", err)
		return
	}
	ui.Printf("Token %s revoked.\n", args[0])
}
