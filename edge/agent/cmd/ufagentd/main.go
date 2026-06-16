package main

import (
	grpc_private "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_private"
	grpc_public "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/rest"
	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/service"
	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/cli/ui"
)

func main() {
	svc := service.NewService().(*service.AppService)

	// Initialize and start the gRPC public server (orchestrator/agent-agent communication)
	grpcPublicServer := grpc_public.AgentGRPCPublicServer{Service: svc}
	go grpcPublicServer.Serve()

	// Initialize and start the gRPC private server (CLI access via unix socket)
	grpcPrivateServer := grpc_private.AgentGRPCPrivateServer{Service: svc}
	go grpcPrivateServer.Serve()

	// Initialize and start the REST server (for future use, e.g., health checks)
	restServer := rest.AgentRESTServer{Service: svc}
	go restServer.Serve()
	ui.Printf("Edge agent started successfully\n")

	// Block main goroutine to keep servers running
	select {}
}
