package rest

import (
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/gin-gonic/gin"
)

func (rs *RESTServer) RegisterConnectionRoutes(router *gin.RouterGroup) {
	connections := router.Group("/connections")
	connections.POST("/", createConnectionHandler)
	connections.GET("/:id", getConnectionHandler)
	connections.GET("/", listConnectionsHandler)
	connections.POST("/terminate", terminateConnectionHandler)
	connections.POST("/stream/new", newStreamHandler)
	connections.POST("/stream/close", closeStreamHandler)
}

func createConnectionHandler(c *gin.Context) {
	svc := getServiceFromContext(c.Request.Context())
	var req types.CreateConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	conn, err := svc.NewConnection(c.Request.Context(), req)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to create connection: " + err.Error()})
		return
	}

	c.JSON(200, conn)
}

func getConnectionHandler(c *gin.Context) {
	svc := getServiceFromContext(c.Request.Context())
	id := c.Param("id")
	conn, err := svc.GetConnection(c.Request.Context(), id)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to get connection: " + err.Error()})
		return
	}

	c.JSON(200, conn)
}

func listConnectionsHandler(c *gin.Context) {
	svc := getServiceFromContext(c.Request.Context())
	connections, err := svc.ListConnections(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to list connections: " + err.Error()})
		return
	}

	c.JSON(200, connections)
}

func terminateConnectionHandler(c *gin.Context) {
	svc := getServiceFromContext(c.Request.Context())
	var req types.TerminateConnRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	resp, err := svc.TerminateConnection(c.Request.Context(), req)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to terminate connection: " + err.Error()})
		return
	}

	c.JSON(200, resp)
}

func newStreamHandler(c *gin.Context) {
	svc := getServiceFromContext(c.Request.Context())
	var req types.NewStreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	stream, err := svc.NewStream(c.Request.Context(), req)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to create new stream: " + err.Error()})
		return
	}

	c.JSON(200, stream)
}

func closeStreamHandler(c *gin.Context) {
	svc := getServiceFromContext(c.Request.Context())
	var req types.CloseStreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "invalid request body"})
		return
	}

	resp, err := svc.CloseStream(c.Request.Context(), req)
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to close stream: " + err.Error()})
		return
	}

	c.JSON(200, resp)
}
