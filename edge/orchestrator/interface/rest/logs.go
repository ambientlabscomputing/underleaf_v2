package rest

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
)

var wsUpgrader = websocket.Upgrader{
	// Allow all origins for dev; tighten in production.
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *OrchestratorRESTServer) RegisterLogRoutes(router *gin.RouterGroup, _ *service.AppService) {
	router.GET("/containers/:dockerId/logs", s.GetContainerLogs)
	router.GET("/containers/:dockerId/logs/stream", s.StreamContainerLogs)
}

// GetContainerLogs handles GET /api/v2/containers/:dockerId/logs
// Query params: since_ms, until_ms, limit, cursor_id
func (s *OrchestratorRESTServer) GetContainerLogs(c *gin.Context) {
	dockerID := c.Param("dockerId")
	sinceMs := parseInt64Query(c, "since_ms", 0)
	untilMs := parseInt64Query(c, "until_ms", 0)
	limit := int(parseInt64Query(c, "limit", 0))
	cursorID := parseInt64Query(c, "cursor_id", 0)

	page, err := s.Service.Logs().GetLogs(c.Request.Context(), dockerID, sinceMs, untilMs, limit, cursorID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, page)
}

// StreamContainerLogs handles GET /api/v2/containers/:dockerId/logs/stream (WebSocket)
// Query param: since_ms (optional, 0 = tail only)
func (s *OrchestratorRESTServer) StreamContainerLogs(c *gin.Context) {
	dockerID := c.Param("dockerId")
	sinceMs := parseInt64Query(c, "since_ms", 0)

	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade writes its own error response.
		return
	}
	defer conn.Close()

	ctx := c.Request.Context()
	ch, err := s.Service.Logs().StreamLogs(ctx, dockerID, sinceMs)
	if err != nil {
		_ = conn.WriteJSON(gin.H{"error": err.Error()})
		return
	}

	for line := range ch {
		if err := conn.WriteJSON(line); err != nil {
			return
		}
	}
}

func parseInt64Query(c *gin.Context, key string, def int64) int64 {
	raw := c.Query(key)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return def
	}
	return v
}
