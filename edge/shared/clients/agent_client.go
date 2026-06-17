package clients

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	agentpb "github.com/ambientlabscomputing/underleaf_v2/edge/agent/interface/grpc_public"
)

const agentAddr = "localhost:50101"

// AgentClient is a gRPC client for the AgentPublic service.
type AgentClient struct {
	client agentpb.AgentPublicClient
	conn   *grpc.ClientConn
}

func NewAgentClient() (*AgentClient, error) {
	conn, err := grpc.NewClient(agentAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &AgentClient{
		client: agentpb.NewAgentPublicClient(conn),
		conn:   conn,
	}, nil
}

// Ping calls the agent's Ping RPC and returns the result.
func (c *AgentClient) Ping(ctx context.Context, source string) (*PingResult, error) {
	resp, err := c.client.Ping(ctx, &agentpb.PingRequest{
		Source:          source,
		TimestampUnixMs: time.Now().UnixMilli(),
	})
	if err != nil {
		return nil, err
	}
	return &PingResult{Responder: resp.Responder, TimestampUnixMs: resp.TimestampUnixMs}, nil
}

// Close releases the underlying gRPC connection.
func (c *AgentClient) Close() {
	c.conn.Close()
}
