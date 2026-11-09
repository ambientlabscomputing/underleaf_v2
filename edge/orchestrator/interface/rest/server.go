package rest

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

type OrchestratorRESTServer struct {
	Service *service.AppService

	// UIDir is a built Orchestrator UI to serve from the same listener as the
	// API (one origin, so no CORS). Empty means the value of $UNDERLEAF_UI_DIR;
	// if that is empty too, only the API is served (dev: `vite` proxies /api).
	UIDir string

	// logSrc overrides the log relay (tests); nil means Service.Logs().
	logSrc logSource
	// tokenSrc overrides bearer-token verification (tests); nil means Service.Tokens().
	tokenSrc tokenVerifier
	// Remote authenticates cloud-signed assertions for the remote listener
	// (step 7b). It is only consulted by the remote router; the local router
	// never reads it. nil disables the remote listener.
	Remote remoteAuthenticator

	// ticketSrc holds WebSocket tickets; created on first use.
	ticketSrc  *ticketStore
	ticketOnce sync.Once
}

// envUIDir names the directory holding the built UI (index.html + assets).
const envUIDir = "UNDERLEAF_UI_DIR"

// newRouter builds the single router that serves both the API (under basePath)
// and, if uiDir is set, the UI. Every request, including static assets and
// WebSocket upgrades, goes through requestGuard.
func (s *OrchestratorRESTServer) newRouter(basePath, uiDir string) *gin.Engine {
	return s.buildRouter(basePath, uiDir, listenerLocal)
}

// listenerKind says which listener a router serves. The two never share a
// middleware chain: the local one has no assertion support at all and the
// remote one has no unauthenticated path.
type listenerKind int

const (
	listenerLocal listenerKind = iota
	listenerRemote
)

func (s *OrchestratorRESTServer) buildRouter(basePath, uiDir string, kind listenerKind) *gin.Engine {
	router := gin.New()
	router.Use(gin.LoggerWithConfig(gin.LoggerConfig{Formatter: redactingLogFormatter}), gin.Recovery())

	router.Use(securityHeaders())
	if kind == listenerRemote {
		router.Use(remoteGuard())
		router.Use(s.remoteAuthMiddleware(basePath))
	} else {
		router.Use(rejectRemoteCredentials())
		router.Use(requestGuard())
		router.Use(s.authMiddleware(basePath))
	}
	// No CORS middleware: the UI is served from this origin (or, in dev,
	// proxied through the Vite server), so browsers never make cross-origin
	// requests to the API. Cross-origin callers are rejected by requestGuard.

	v2 := router.Group(basePath)
	s.RegisterNodeRoutes(v2, s.Service)
	s.RegisterContainerRoutes(v2, s.Service)
	s.RegisterLogRoutes(v2, s.Service)
	s.RegisterDeploymentRoutes(v2, s.Service)
	s.RegisterAuthRoutes(v2)
	health := v2.Group("/health")
	health.GET("", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "OK"})
	})

	registerUI(router, basePath, uiDir)
	return router
}

func (s *OrchestratorRESTServer) Serve() {
	config := utils.GetConfig(utils.OrchestratorConfig)

	uiDir := s.UIDir
	if uiDir == "" {
		uiDir = os.Getenv(envUIDir)
	}
	router := s.newRouter(config.Http.BasePath.String(), uiDir)
	if uiDir != "" {
		fmt.Printf("Serving UI from %s on the same origin as the API\n", uiDir)
	} else {
		fmt.Printf("%s not set: serving the API only\n", envUIDir)
	}

	// The REST API is unauthenticated, so it stays on loopback (the Docker
	// image is the one recorded exception; see utils.CheckLocalBind).
	if err := utils.CheckLocalBind(utils.BindAddr()); err != nil {
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
