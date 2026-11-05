//go:generate protoc --go_out=. --go_opt=paths=source_relative agent_public.proto
//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative agent_link.proto

// Package grpc_public contains the generated protobuf types for the agent's
// command vocabulary (agent_public.proto) and the AgentLink service
// (agent_link.proto) through which the Orchestrator commands an agent. The agent
// does not serve any gRPC service: it dials out to the Orchestrator and opens
// the link (see edge/agent/link and edge/orchestrator/link).
package grpc_public
