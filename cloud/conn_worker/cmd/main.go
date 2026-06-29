package main

import (
	"context"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_v2/cloud/conn_worker/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

func main() {
	config := utils.GetConfig(utils.ConnWorkerConfig)
	service := service.NewService(config)
	if service == nil {
		fmt.Println("Failed to create service")
		return
	}

	err := service.Start(context.Background())
	if err != nil {
		fmt.Println("Failed to start service:", err)
		return
	}
}
