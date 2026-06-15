package rest

import (
	"net/http"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/types"
	"github.com/gin-gonic/gin"
)

func (s *OrchestratorRESTServer) RegisterNodeRoutes(router *gin.RouterGroup, service *service.AppService) {
	router.GET("/nodes", s.GetNodes)
}

type GetNodesResponse struct {
	Results []*types.Node `json:"results"`
}

func (s *OrchestratorRESTServer) GetNodes(c *gin.Context) {
	nodes, err := s.Service.Nodes().GetNodes()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, &GetNodesResponse{Results: nodes})
}
