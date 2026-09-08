package rest

import (
	"errors"
	"net/http"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/gin-gonic/gin"
)

func (s *OrchestratorRESTServer) RegisterDeploymentRoutes(router *gin.RouterGroup, service *service.AppService) {
	router.POST("/deployments", s.CreateDeployment)
	router.GET("/deployments", s.GetDeployments)
	router.GET("/deployments/:id", s.GetDeployment)
}

// DeployRequest is the POST /deployments body.
type DeployRequest struct {
	Source string `json:"source" binding:"required"` // "gh:<owner>/<repo>[@ref]"
	Ref    string `json:"ref,omitempty"`
	Token  string `json:"token,omitempty"`
}

// CreateDeployment resolves a manifest from a source repo and persists it
// (fast, synchronous), then reconciles it onto the agent in the background —
// the response's status is always "in_progress"; poll GET /deployments/:id
// for the outcome.
func (s *OrchestratorRESTServer) CreateDeployment(c *gin.Context) {
	var req DeployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	deployment, err := s.Service.DeployFromSource(c.Request.Context(), req.Source, req.Ref, req.Token)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, deployment)
}

type GetDeploymentsResponse struct {
	Results []*types.Deployment `json:"results"`
}

// GetDeployments returns all tracked deployments.
func (s *OrchestratorRESTServer) GetDeployments(c *gin.Context) {
	deployments, err := s.Service.Deployments().GetDeployments()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, &GetDeploymentsResponse{Results: deployments})
}

// GetDeployment returns a single deployment by ID.
func (s *OrchestratorRESTServer) GetDeployment(c *gin.Context) {
	deployment, err := s.Service.Deployments().GetDeployment(c.Param("id"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, repository.ErrDeploymentNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, deployment)
}
