package rest

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/utils"
)

type OrchestratorRESTServer struct {
	Service *service.AppService
}

func (s *OrchestratorRESTServer) Serve() {
	router := gin.Default()

	router.GET("/api/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "OK"})
	})

	v2 := router.Group("/api/v2")
	s.RegisterNodeRoutes(v2, s.Service)

	config := utils.GetConfig(utils.OrchestratorConfig)
	addr := fmt.Sprintf(":%d", config.Http.Port)

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  utils.ParseDuration(config.Http.ReadTimeout),
		WriteTimeout: utils.ParseDuration(config.Http.WriteTimeout),
		IdleTimeout:  utils.ParseDuration(config.Http.IdleTimeout),
	}

	fmt.Printf("Server actively listening on http://localhost:%d\n", config.Http.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(fmt.Sprintf("Server failed to start: %v", err))
	}
}
