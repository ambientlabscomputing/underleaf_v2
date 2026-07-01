package rest

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

type OrchestratorRESTServer struct {
	Service *service.AppService
}

func (s *OrchestratorRESTServer) Serve() {
	config := utils.GetConfig(utils.OrchestratorConfig)

	router := gin.Default()

	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://localhost:5183"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	v2 := router.Group(config.Http.BasePath.String())
	s.RegisterNodeRoutes(v2, s.Service)
	s.RegisterContainerRoutes(v2, s.Service)
	s.RegisterLogRoutes(v2, s.Service)
	health := v2.Group("/health")
	health.GET("", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "OK"})
	})

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
