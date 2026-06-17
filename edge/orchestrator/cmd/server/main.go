package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_private"
	grpc_public_server "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_public/server"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/rest"
	_ "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository/migrations"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/spf13/cobra"
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
	svc := service.NewService().(*service.AppService)

	// Initialize and start the REST server
	restServer := rest.OrchestratorRESTServer{Service: svc}
	go restServer.Serve()

	// Initialize and start the gRPC public server (agent communication)
	grpcPublicServer := grpc_public_server.OrchestratorGRPCPublicServer{Service: svc}
	go grpcPublicServer.Serve()

	// Initialize and start the gRPC private server (CLI access via unix socket)
	grpcPrivateServer := grpc_private.OrchestratorGRPCPrivateServer{Service: svc}
	go grpcPrivateServer.Serve()

	// Ping the agent on startup to verify connectivity.
	go func() {
		ctx := context.Background()
		for i := 0; i < 20; i++ {
			result, err := svc.Health().PingPeer(ctx)
			if err != nil {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			fmt.Printf("[health] ping → agent OK (responder=%s, rtt≈%dms)\n",
				result.Responder, time.Now().UnixMilli()-result.TimestampUnixMs)
			return
		}
		fmt.Println("[health] warning: agent did not respond within startup window")
	}()

	// Block main goroutine to keep servers running
	select {}
}
