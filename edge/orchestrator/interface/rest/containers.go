package rest

import (
	"net/http"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/gin-gonic/gin"
)

func (s *OrchestratorRESTServer) RegisterContainerRoutes(router *gin.RouterGroup, service *service.AppService) {
	router.GET("/containers", s.GetContainers)
	router.GET("/nodes/:nodeId/containers", s.GetContainersByNode)
}

type GetContainersResponse struct {
	Results []*types.Container `json:"results"`
}

// GetContainers returns all containers across all nodes.
func (s *OrchestratorRESTServer) GetContainers(c *gin.Context) {
	containers, err := s.Service.Containers().GetContainers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, &GetContainersResponse{Results: containers})
}

// GetContainersByNode returns containers for a specific node.
func (s *OrchestratorRESTServer) GetContainersByNode(c *gin.Context) {
	nodeId := c.Param("nodeId")
	containers, err := s.Service.Containers().GetContainersByNode(nodeId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, &GetContainersResponse{Results: containers})
}
