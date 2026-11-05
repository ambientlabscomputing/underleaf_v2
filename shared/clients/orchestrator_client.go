package clients

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	orchpb "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/shared/pki"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// OrchestratorClient is a gRPC client for the OrchestratorPublic service.
//
// On a node that has joined a cluster (membership state exists) it talks to the
// cluster listener over mTLS with the node certificate. Otherwise it is the
// single-node install's agent and uses the local, loopback-only listener.
type OrchestratorClient struct {
	client orchpb.OrchestratorPublicClient
	conn   *grpc.ClientConn
	member *pki.Member // nil for the local listener
}

func NewOrchestratorClient() (*OrchestratorClient, error) {
	cfg := utils.GetConfig(utils.AgentConfig)
	member, err := pki.LoadMember(utils.ClusterStateDir(cfg, "node"))
	switch {
	case err == nil:
		conn, err := DialCluster(member)
		if err != nil {
			return nil, err
		}
		return &OrchestratorClient{client: orchpb.NewOrchestratorPublicClient(conn), conn: conn, member: member}, nil
	case errors.Is(err, pki.ErrNotMember):
		// Not joined: single-node install, local listener.
	default:
		// Damaged membership state must not silently turn into "talk to
		// localhost" (that would hide the problem and could register the wrong
		// node).
		return nil, fmt.Errorf("cluster membership state is unreadable: %w", err)
	}
	conn, err := grpc.NewClient(utils.OrchestratorLocalAddr(),
		append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, utils.ClusterAuthDialOptions()...)...)
	if err != nil {
		return nil, err
	}
	return &OrchestratorClient{client: orchpb.NewOrchestratorPublicClient(conn), conn: conn}, nil
}

// NewOrchestratorClientFromConn wraps an existing connection (used to verify a
// fresh enrollment without re-reading state).
func NewOrchestratorClientFromConn(conn *grpc.ClientConn, m *pki.Member) *OrchestratorClient {
	return &OrchestratorClient{client: orchpb.NewOrchestratorPublicClient(conn), conn: conn, member: m}
}

// Member is the node's cluster membership, or nil on the local listener.
func (c *OrchestratorClient) Member() *pki.Member { return c.member }

// Conn is the underlying connection (the agent link and certificate renewal
// share it).
func (c *OrchestratorClient) Conn() *grpc.ClientConn { return c.conn }

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
func (c *OrchestratorClient) CreateNode(ctx context.Context, req types.CreateNodeRequest) (*types.Node, error) {
	resp, err := c.client.CreateNode(ctx, &orchpb.CreateNodeRequest{
		Name:      req.Name,
		IpAddress: req.IPAddr,
		Os:        req.OS,
		Arch:      req.Arch,
	})
	if err != nil {
		return nil, err
	}
	return &types.Node{
		ID:     resp.Id,
		Name:   resp.Name,
		IPAddr: resp.IpAddress,
		OS:     resp.Os,
		Arch:   resp.Arch,
	}, nil
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
			Name:     c.Name,
		})
	}
	_, err := c.client.ReportContainers(ctx, req)
	return err
}
