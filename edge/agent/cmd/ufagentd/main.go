package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_private"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/link"
	_ "github.com/ambientlabscomputing/underleaf_v2/edge/agent/repository/migrations"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/cli/ui"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
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

// Version is reported to the Orchestrator in the link hello.
var Version = "dev"

func Run() {
	svc := service.NewService().(*service.AppService)

	// The agent listens on nothing reachable from the network (RFD 5, decision
	// 5). Its only inbound surface is the owner-only unix socket for the local
	// CLI; everything the Orchestrator asks of it arrives down the agent link,
	// which the agent dials out.
	grpcPrivateServer := grpc_private.AgentGRPCPrivateServer{Service: svc}
	go grpcPrivateServer.Serve()

	// The agent link: dial out to the Orchestrator (cluster listener over mTLS
	// if this node joined a cluster, else the local listener), keep one stream
	// open, run the commands that arrive on it, reconnect forever on a drop.
	cfg := utils.GetConfig(utils.AgentConfig)
	linkClient := &link.Client{
		Dial: link.NewDialer(cfg, func() (string, error) {
			n, err := svc.GetNode()
			if err != nil {
				return "", err
			}
			return n.ID, nil
		}),
		Handler: &link.Dispatcher{Service: svc},
		Version: Version,
		Logf:    func(format string, args ...any) { fmt.Printf(format+"\n", args...) },
	}
	go linkClient.Run(context.Background())

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
	if err := RootCmd.Execute(); err != nil {
		// cobra has already printed the error; scripts (install.sh's join flow)
		// need a non-zero status.
		os.Exit(1)
	}
}
