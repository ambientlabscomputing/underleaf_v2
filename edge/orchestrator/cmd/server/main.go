package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	agentpb "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/cluster"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_private"
	grpc_public_server "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_public/server"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/rest"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/link"
	_ "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository/migrations"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

var RootCmd = &cobra.Command{
	Use:   "orchestrator-server",
	Short: "Start the edge orchestrator server",
	Run: func(cmd *cobra.Command, args []string) {
		Run()
	},
}

var RunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the edge orchestrator server",
	Run: func(cmd *cobra.Command, args []string) {
		Run()
	},
}

func init() {
	RootCmd.AddCommand(RunCmd)
}

func main() {
	RootCmd.Execute()
}

func Run() {
	config := utils.GetConfig(utils.OrchestratorConfig)
	svc := service.NewService(config).(*service.AppService)
	if err := svc.Start(); err != nil {
		panic("Failed to start service: " + err.Error())
	}

	// Cluster membership (RFD 5): the cluster-local CA is generated on first
	// start. The cluster listener (mTLS) only comes up after `orcli nodes add`,
	// or on restart if it was enabled before.
	clusterSvc := svc.Cluster()
	if err := clusterSvc.Start(); err != nil {
		panic("Failed to start cluster membership: " + err.Error())
	}
	clusterServer := &cluster.Server{Service: svc}
	// Worker agents link in over the cluster listener; their identity is the
	// certificate that passed mTLS.
	clusterLink := link.NewServer(svc.Links(), link.ClusterAuth{Identity: cluster.NodeID})
	clusterServer.Register = func(gs *grpc.Server) { agentpb.RegisterAgentLinkServer(gs, clusterLink) }
	clusterSvc.StartListener = clusterServer.Start
	clusterSvc.Linked = svc.Links().Linked
	// `nodes rm` and re-enrollment revoke certificates: cut live links too.
	clusterSvc.OnNodeRemoved = svc.Links().Drop
	clusterSvc.OnCertsRevoked = svc.Links().Drop
	if clusterSvc.ListenerEnabled() {
		if err := clusterServer.Start(clusterSvc.Advertised()); err != nil {
			fmt.Printf("[cluster] warning: could not restore the cluster listener: %v\n", err)
		}
	}
	go func() {
		for range time.Tick(24 * time.Hour) {
			if n, err := clusterSvc.PruneRevoked(); err != nil {
				fmt.Printf("[cluster] deny-list prune failed: %v\n", err)
			} else if n > 0 {
				fmt.Printf("[cluster] pruned %d expired entr(ies) from the revocation deny list\n", n)
			}
		}
	}()

	// Initialize and start the REST server
	restServer := rest.OrchestratorRESTServer{Service: svc}
	go restServer.Serve()
	// Remote access (assertion-gated listener); a no-op unless UNDERLEAF_REMOTE_ADDR is set.
	startRemoteAccess(context.Background(), &restServer, config)

	// Initialize and start the gRPC public server (agent communication)
	grpcPublicServer := grpc_public_server.OrchestratorGRPCPublicServer{
		Service: svc,
		// Single-node install: the manager's own agent links in over the
		// loopback-only local listener.
		Link: link.NewServer(svc.Links(), link.LocalAuth{Nodes: svc.Repository.Nodes}),
	}
	go grpcPublicServer.Serve()

	// Initialize and start the gRPC private server (CLI access via unix socket)
	grpcPrivateServer := grpc_private.OrchestratorGRPCPrivateServer{Service: svc}
	go grpcPrivateServer.Serve()

	// Ping the agent on startup to verify connectivity.
	go func() {
		ctx := context.Background()
		for i := 0; i < 60; i++ {
			result, err := svc.Health().PingPeer(ctx)
			if err != nil {
				time.Sleep(time.Second)
				continue
			}
			fmt.Printf("[health] ping → agent OK (responder=%s, rtt≈%dms)\n",
				result.Responder, time.Now().UnixMilli()-result.TimestampUnixMs)
			return
		}
		fmt.Println("[health] warning: no agent linked within the startup window (agents dial in; see `orcli nodes ls`)")
	}()

	// Start container sync task (periodic trigger for agent to ingest containers).
	go func() {
		intervalStr := os.Getenv("CONTAINER_SYNC_INTERVAL_SECS")
		interval := 60 // default 60 seconds
		if intervalStr != "" {
			if parsed, err := strconv.Atoi(intervalStr); err == nil && parsed > 0 {
				interval = parsed
			}
		}
		if interval <= 0 {
			fmt.Println("[sync] container sync disabled (interval <= 0)")
			return
		}
		fmt.Printf("[sync] container sync task started (interval=%ds)\n", interval)

		ticker := time.NewTicker(time.Duration(interval) * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			count, err := svc.Health().IngestAgentContainers(ctx)
			cancel()
			if err != nil {
				fmt.Printf("[sync] container ingest failed: %v\n", err)
			} else {
				fmt.Printf("[sync] container ingest complete: %d container(s) synced\n", count)
			}
		}
	}()

	// Start cert renewal check task (periodic check, renews if within threshold of expiry).
	go func() {
		intervalStr := os.Getenv("CERT_RENEWAL_CHECK_INTERVAL_SECS")
		interval := 6 * 60 * 60 // default 6 hours -- certs are valid 730 days, no need to check more often
		if intervalStr != "" {
			if parsed, err := strconv.Atoi(intervalStr); err == nil && parsed > 0 {
				interval = parsed
			}
		}
		if interval <= 0 {
			fmt.Println("[cert-renewal] cert renewal check disabled (interval <= 0)")
			return
		}
		fmt.Printf("[cert-renewal] cert renewal check task started (interval=%ds)\n", interval)

		ticker := time.NewTicker(time.Duration(interval) * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err := svc.Registration().CheckAndRenewCertificate(ctx, config.CertDir)
			cancel()
			if err != nil {
				fmt.Printf("[cert-renewal] check failed: %v\n", err)
			}
		}
	}()

	// Block main goroutine to keep servers running
	select {}
}
