package grpc_private

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/pki"
)

//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative cluster_private.proto

// clusterPrivate implements the CLI-facing cluster-membership RPCs on the
// owner-only unix socket.
type clusterPrivate struct {
	UnimplementedClusterPrivateServer
	Service *service.AppService
}

// AddNode implements `orcli nodes add`.
func (s *clusterPrivate) AddNode(_ context.Context, req *AddNodeRequest) (*AddNodeResponse, error) {
	res, err := s.Service.Cluster().AddNode(req.GetAdvertise())
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return &AddNodeResponse{
		JoinString:    res.JoinString,
		Advertise:     res.Advertise,
		CaFingerprint: pki.FormatFingerprint(res.Fingerprint),
		ExpiresAtUnix: res.ExpiresAt.Unix(),
	}, nil
}

// RemoveNode implements `orcli nodes rm`.
func (s *clusterPrivate) RemoveNode(_ context.Context, req *RemoveNodeRequest) (*RemoveNodeResponse, error) {
	n, err := s.Service.Cluster().RemoveNode(req.GetNodeId())
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &RemoveNodeResponse{RevokedCertificates: int32(n)}, nil
}

// ListClusterNodes implements `orcli nodes ls`.
func (s *clusterPrivate) ListClusterNodes(_ context.Context, _ *ListClusterNodesRequest) (*ListClusterNodesResponse, error) {
	infos, err := s.Service.Cluster().ListNodes()
	if err != nil {
		return nil, err
	}
	resp := &ListClusterNodesResponse{}
	for _, i := range infos {
		n := &ClusterNode{
			Id:           i.Node.ID,
			Name:         i.Node.Name,
			IpAddress:    i.Node.IPAddr,
			Os:           i.Node.OS,
			Arch:         i.Node.Arch,
			Transport:    i.Node.Transport,
			CertExpiring: i.Warn,
			CertExpired:  i.Expired,
			Linked:       s.Service.Cluster().IsLinked(i.Node.ID),
		}
		if !i.NotAfter.IsZero() {
			n.CertNotAfterUnix = i.NotAfter.Unix()
		}
		resp.Nodes = append(resp.Nodes, n)
	}
	return resp, nil
}
