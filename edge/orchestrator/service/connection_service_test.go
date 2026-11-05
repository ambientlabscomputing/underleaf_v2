package service

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NewStream must refuse control-plane ports before touching the cloud or the
// agent. The clients are deliberately nil: reaching either would panic, so a
// clean InvalidArgument proves the check runs first.
func TestNewStreamRejectsControlPlanePortsAtCreation(t *testing.T) {
	t.Setenv("UNDERLEAF_CONFIG", "")
	t.Setenv("UNDERLEAF_EXTRA_CONTROL_PORTS", "15090")
	s := NewConnectionService(nil, nil)

	for _, port := range []int{9090, 9091, 50100, 50101, 50103, 2375, 15090, 0, -1, 70000} {
		stream, err := s.NewStream(context.Background(), "node-1", "gateway", port)
		if stream != nil {
			t.Fatalf("port %d: no stream may be created", port)
		}
		if got := status.Code(err); got != codes.InvalidArgument {
			t.Fatalf("port %d: want InvalidArgument, got %v (%v)", port, got, err)
		}
	}
}
