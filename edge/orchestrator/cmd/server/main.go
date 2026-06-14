package main

import (
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_private"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/rest"
)

func main() {
	// Initialize and start the REST server
	restServer := rest.OrchestratorRESTServer{}
	go restServer.Serve()

	// Initialize and start the gRPC public server
	grpcPublicServer := grpc_public.OrchestratorGRPCPublicServer{}
	go grpcPublicServer.Serve()

	// Initialize and start the gRPC private server
	grpcPrivateServer := grpc_private.OrchestratorGRPCPrivateServer{}
	go grpcPrivateServer.Serve()

	// Block main goroutine to keep servers running
	select {}
}
