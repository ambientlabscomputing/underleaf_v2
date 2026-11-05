//go:build devmode

package grpc_private

import "context"

// SqlQuery implements OrchestratorPrivateServer. It exists only in devmode
// builds (matching the `orcli sql` command); release builds fall through to
// UnimplementedOrchestratorPrivateServer, so the RPC returns Unimplemented.
// Queries run on a read-only connection (see Repository.Query).
func (s *OrchestratorGRPCPrivateServer) SqlQuery(ctx context.Context, req *SqlQueryRequest) (*SqlQueryResponse, error) {
	result, err := s.Service.Query(req.Query)
	if err != nil {
		return nil, err
	}
	return &SqlQueryResponse{Result: result}, nil
}
