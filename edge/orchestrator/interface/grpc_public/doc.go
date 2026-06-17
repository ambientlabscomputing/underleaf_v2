//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative orchestrator_public.proto

// Package grpc_public contains the generated protobuf types for the OrchestratorPublic gRPC service.
// The server implementation lives in the server/ subpackage.
package grpc_public
