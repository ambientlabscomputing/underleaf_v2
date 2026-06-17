package clients

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	orchpb "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/edge/shared/types"
)

const orchestratorAddr = "localhost:50100"

// OrchestratorClient is a gRPC client for the OrchestratorPublic service.
type OrchestratorClient struct {
	client orchpb.OrchestratorPublicClient
	conn   *grpc.ClientConn
}

func NewOrchestratorClient() (*OrchestratorClient, error) {
	conn, err := grpc.NewClient(orchestratorAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &OrchestratorClient{
		client: orchpb.NewOrchestratorPublicClient(conn),
		conn:   conn,
	}, nil
}

// Ping calls the orchestrator's Ping RPC and returns the result.
func (c *OrchestratorClient) Ping(ctx context.Context, source string) (*PingResult, error) {
	resp, err := c.client.Ping(ctx, &orchpb.PingRequest{
		Source:          source,
		TimestampUnixMs: time.Now().UnixMilli(),
	})
	if err != nil {
		return nil, err
	}
	return &PingResult{Responder: resp.Responder, TimestampUnixMs: resp.TimestampUnixMs}, nil
}

// CreateNode calls the orchestrator's CreateNode RPC and returns the result.
func (c *OrchestratorClient) CreateNode(ctx context.Context, name string) (*types.Node, error) {
	resp, err := c.client.CreateNode(ctx, &orchpb.CreateNodeRequest{Name: name})
	if err != nil {
		return nil, err
	}
	return &types.Node{Name: resp.Name, ID: resp.Id}, nil
}

// Close releases the underlying gRPC connection.
func (c *OrchestratorClient) Close() {
	c.conn.Close()
}

// ReportContainers pushes a list of Docker containers from an agent to the orchestrator.
func (c *OrchestratorClient) ReportContainers(ctx context.Context, nodeID string, containers []*types.Container) error {
	req := &orchpb.ReportContainersRequest{
		NodeId: nodeID,
	}
	for _, c := range containers {
		req.Containers = append(req.Containers, &orchpb.Container{
			Id:       c.ID,
			DockerId: c.DockerID,
			NodeId:   string(c.NodeID),
			Image:    c.Image,
			Status:   c.Status,
			Uptime:   c.Uptime,
		})
	}
	_, err := c.client.ReportContainers(ctx, req)
	return err
}
