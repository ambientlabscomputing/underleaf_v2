package service

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ConnectionService.NewStream fails fast on control-plane ports. connClient is
// nil on purpose: reaching it would panic.
func TestAgentNewStreamRejectsControlPlanePorts(t *testing.T) {
	t.Setenv("UNDERLEAF_CONFIG", "")
	t.Setenv("UNDERLEAF_EXTRA_CONTROL_PORTS", "")
	s := &ConnectionService{}
	for _, port := range []int{9090, 9091, 50100, 50101, 50103, 2375, 0} {
		err := s.NewStream(context.Background(), NewStreamRequest{ConnectionID: "c", StreamID: "s", Type: "gateway", Port: port})
		if got := status.Code(err); got != codes.PermissionDenied {
			t.Fatalf("port %d: want PermissionDenied, got %v (%v)", port, got, err)
		}
	}
}
