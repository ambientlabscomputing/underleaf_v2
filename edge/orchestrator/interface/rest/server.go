package rest

import (
	"fmt"
	"net/http"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/utils"
)

type OrchestratorRESTServer struct{}

func (s *OrchestratorRESTServer) Serve() {
	// Initialize a new local multiplexer instance
	mux := http.NewServeMux()

	// 1. Static Route with exact matching
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"OK"}`))
	})

	// Configure the production HTTP Server profile
	config := utils.GetConfig()
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", config.Http.Port),
		Handler:      mux, // Inject the configured mux instance
		ReadTimeout:  utils.ParseDuration(config.Http.ReadTimeout),
		WriteTimeout: utils.ParseDuration(config.Http.WriteTimeout),
		IdleTimeout:  utils.ParseDuration(config.Http.IdleTimeout),
	}

	fmt.Printf("Server actively listening on http://localhost:%d\n", config.Http.Port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(fmt.Sprintf("Server failed to start: %v", err))
	}
}
