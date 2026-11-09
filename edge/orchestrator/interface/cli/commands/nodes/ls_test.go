package nodes

import (
	"strings"
	"testing"
	"time"

	grpc_private "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_private"
)

func TestCertColumn(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	mk := func(daysLeft int, expiring, expired bool) *grpc_private.ClusterNode {
		return &grpc_private.ClusterNode{
			CertNotAfterUnix: now.Add(time.Duration(daysLeft) * 24 * time.Hour).Unix(),
			CertExpiring:     expiring,
			CertExpired:      expired,
		}
	}
	if cell, warn := certColumn(&grpc_private.ClusterNode{}, now); cell != "-" || warn != "" {
		t.Errorf("local node: %q %q", cell, warn)
	}
	if cell, warn := certColumn(mk(400, false, false), now); warn != "" || !strings.Contains(cell, "(400d)") {
		t.Errorf("healthy: %q %q", cell, warn)
	}
	if cell, warn := certColumn(mk(29, true, false), now); warn == "" || !strings.HasSuffix(cell, "!") {
		t.Errorf("29 days must warn: %q %q", cell, warn)
	}
	if cell, warn := certColumn(mk(-3, true, true), now); warn != "has expired" || !strings.Contains(cell, "EXPIRED") {
		t.Errorf("expired: %q %q", cell, warn)
	}
}
