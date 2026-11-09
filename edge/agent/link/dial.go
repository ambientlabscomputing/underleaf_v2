package link

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/pki"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// NewDialer returns the production Dial function.
//
// It re-reads the membership state on every (re)connect:
//   - a node that joined a cluster dials the cluster listener over mTLS with its
//     node certificate, as the node id in that certificate;
//   - a node that has not joined is the single-node install's agent: it dials the
//     local listener (loopback, UNDERLEAF_ORCHESTRATOR_ADDR) and names its node
//     id, taken from the agent's own database (set by `ufagent register`).
//
// Damaged membership state is an error, never a silent fallback to the local
// listener.
func NewDialer(cfg utils.Config, localNodeID func() (string, error)) func(ctx context.Context) (*Target, error) {
	return func(ctx context.Context) (*Target, error) {
		member, err := pki.LoadMember(utils.ClusterStateDir(cfg, "node"))
		switch {
		case err == nil:
			conn, err := clients.DialCluster(member)
			if err != nil {
				return nil, err
			}
			return &Target{Conn: conn, NodeID: member.State.NodeID}, nil
		case errors.Is(err, pki.ErrNotMember):
		default:
			return nil, fmt.Errorf("cluster membership state is unreadable: %w", err)
		}

		id, err := localNodeID()
		if err != nil {
			return nil, fmt.Errorf("this node is not registered with the Orchestrator yet (run `ufagent register`): %w", err)
		}
		conn, err := grpc.NewClient(utils.OrchestratorLocalAddr(),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 30 * time.Second, Timeout: 10 * time.Second, PermitWithoutStream: true}),
		)
		if err != nil {
			return nil, err
		}
		return &Target{Conn: conn, NodeID: id, Local: true}, nil
	}
}
