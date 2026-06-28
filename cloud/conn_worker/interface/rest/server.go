package rest

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ambientlabscomputing/underleaf_v2/cloud/conn_worker/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

type RESTServer struct {
	service service.Service
	engine  *gin.Engine
}

func NewRESTServer(service service.Service) *RESTServer {
	router := gin.Default()
	return &RESTServer{
		service: service,
		engine:  router,
	}
}

func (rs *RESTServer) Serve() error {
	// Define your REST API routes here
	router := rs.engine.Group("/v1")
	router.Use(rs.DependencyInjector())
	rs.RegisterConnectionRoutes(router)
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// Start the REST server
	config := utils.GetConfig(utils.ConnWorkerConfig)
	port := config.Http.Port
	return rs.engine.Run(":" + strconv.Itoa(port))
}

func (rs *RESTServer) DependencyInjector() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, "service", rs.service)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func getServiceFromContext(ctx context.Context) service.Service {
	svc, ok := ctx.Value("service").(service.Service)
	if !ok {
		utils.Logger.ErrorContext(ctx, "failed to get service from context")
		return nil
	}
	return svc
}
