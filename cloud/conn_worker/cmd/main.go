package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	conngrpc "github.com/ambientlabscomputing/underleaf_v2/cloud/conn_worker/interface/grpc"
	"github.com/ambientlabscomputing/underleaf_v2/cloud/conn_worker/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	config := utils.GetConfig(utils.ConnWorkerConfig)
	svc := service.NewService(config)
	if svc == nil {
		fmt.Println("Failed to create service")
		return
	}

	if err := svc.Start(ctx); err != nil {
		fmt.Println("Failed to start service:", err)
		return
	}

	grpcServer := conngrpc.NewConnWorkerGRPCServer(svc, config)
	go grpcServer.Serve()

	<-ctx.Done()
	utils.Logger.Info("shutting down conn-worker")
	svc.Stop(context.Background())
}
