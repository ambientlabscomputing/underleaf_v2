package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

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

	<-ctx.Done()
	utils.Logger.Info("shutting down conn-worker")
	svc.Stop(context.Background())
}
