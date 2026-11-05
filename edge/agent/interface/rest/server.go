package rest

import (
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

type AgentRESTServer struct {
	Service *service.AppService
}

func (s *AgentRESTServer) Serve() {
	config := utils.GetConfig(utils.AgentConfig)
	router := gin.Default()

	v1 := router.Group(config.Http.BasePath.String())
	v1.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "OK"})
	})

	if err := utils.CheckBind(utils.BindAddr(), false); err != nil {
		panic(fmt.Sprintf("Server failed to start: %v", err))
	}
	addr := net.JoinHostPort(utils.BindAddr(), strconv.Itoa(int(config.Http.Port)))

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  utils.ParseDuration(config.Http.ReadTimeout),
		WriteTimeout: utils.ParseDuration(config.Http.WriteTimeout),
		IdleTimeout:  utils.ParseDuration(config.Http.IdleTimeout),
	}

	fmt.Printf("Server actively listening on http://%s\n", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(fmt.Sprintf("Server failed to start: %v", err))
	}
}
