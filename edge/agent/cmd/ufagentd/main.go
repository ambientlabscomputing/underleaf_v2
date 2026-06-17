package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_private"
	grpc_public_server "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public/server"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/rest"
	_ "github.com/ambientlabscomputing/underleaf_v2/edge/agent/repository/migrations"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/service"
	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/cli/ui"
	"github.com/spf13/cobra"
)

var RootCmd = &cobra.Command{
	Use:   "ufagentd",
	Short: "Edge agent daemon for managing edge nodes and workloads",
}

var RunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the edge agent daemon",
	Run: func(cmd *cobra.Command, args []string) {
		Run()
	},
}

func init() {
	RootCmd.AddCommand(RunCmd)
}

func Run() {
	svc := service.NewService().(*service.AppService)

	// Initialize and start the gRPC public server (orchestrator communication)
	grpcPublicServer := grpc_public_server.AgentGRPCPublicServer{Service: svc}
	go grpcPublicServer.Serve()

	// Initialize and start the gRPC private server (CLI access via unix socket)
	grpcPrivateServer := grpc_private.AgentGRPCPrivateServer{Service: svc}
	go grpcPrivateServer.Serve()

	// Initialize and start the REST server
	restServer := rest.AgentRESTServer{Service: svc}
	go restServer.Serve()

	ui.Printf("Edge agent started successfully\n")

	// Ping the orchestrator on startup to verify connectivity.
	go func() {
		ctx := context.Background()
		for i := 0; i < 20; i++ {
			result, err := svc.Health().PingPeer(ctx)
			if err != nil {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			fmt.Printf("[health] ping → orchestrator OK (responder=%s, rtt≈%dms)\n",
				result.Responder, time.Now().UnixMilli()-result.TimestampUnixMs)
			return
		}
		fmt.Println("[health] warning: orchestrator did not respond within startup window")
	}()

	// Block main goroutine to keep servers running
	select {}
}

func main() {
	RootCmd.Execute()
}
