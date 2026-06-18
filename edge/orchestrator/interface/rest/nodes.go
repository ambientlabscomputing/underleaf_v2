package rest

import (
	"net/http"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/gin-gonic/gin"
)

func (s *OrchestratorRESTServer) RegisterNodeRoutes(router *gin.RouterGroup, service *service.AppService) {
	router.GET("/nodes", s.GetNodes)
}

func (s *OrchestratorRESTServer) GetNodes(c *gin.Context) {
	var query types.QueryNodesRequest

	// Bind the query parameters directly to the struct
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := s.Service.Nodes().GetNodes(query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}
