package rest

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

var wsUpgrader = websocket.Upgrader{
	// Browsers always send Origin on WebSocket handshakes and aren't bound by
	// CORS here, so enforce the same allowlist as the REST API. Non-browser
	// clients omit Origin.
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || originPermitted(origin, r.Host)
	},
}

// logSource is the log relay the handlers use (the service in production).
type logSource interface {
	GetLogs(ctx context.Context, dockerID string, sinceMs, untilMs int64, limit int, cursorID int64) (*service.LogPage, error)
	StreamLogs(ctx context.Context, dockerID string, sinceMs int64) (<-chan *types.LogLine, error)
}

func (s *OrchestratorRESTServer) logs() logSource {
	if s.logSrc != nil {
		return s.logSrc
	}
	return s.Service.Logs()
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

	page, err := s.logs().GetLogs(c.Request.Context(), dockerID, sinceMs, untilMs, limit, cursorID)
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

	upgrader := wsUpgrader
	if isRemote(c) {
		// Behind the cloud the browser's Origin is the cloud's, not ours; the
		// request was already authenticated by its signed assertion.
		upgrader.CheckOrigin = func(*http.Request) bool { return true }
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade writes its own error response.
		return
	}
	defer conn.Close()

	ctx := c.Request.Context()
	ch, err := s.logs().StreamLogs(ctx, dockerID, sinceMs)
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
