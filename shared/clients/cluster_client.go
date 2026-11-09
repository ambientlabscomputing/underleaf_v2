package clients

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"

	orchpb "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/shared/pki"
)

// DialCluster opens an mTLS gRPC connection from a member node to the cluster
// listener. The connection is lazy (grpc.NewClient) and re-dials on its own.
// The certificate is read through the Member on every handshake, so a renewed
// certificate is used by new connections without a restart.
func DialCluster(m *pki.Member, extra ...grpc.DialOption) (*grpc.ClientConn, error) {
	cfg, err := m.TLSConfig()
	if err != nil {
		return nil, err
	}
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(cfg)),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 30 * time.Second, Timeout: 10 * time.Second, PermitWithoutStream: true}),
	}
	return grpc.NewClient(m.State.ManagerAddr, append(opts, extra...)...)
}

// RenewIfDue renews the member's certificate over conn when it is within
// pki.RenewalWindow of expiry. It reports whether a renewal happened. Callers
// run it on a daily timer: a failure is returned for logging and retried the
// next day, which is what gives a node ~90 attempts before expiry.
func RenewIfDue(ctx context.Context, conn grpc.ClientConnInterface, m *pki.Member, now time.Time) (bool, error) {
	if m.NotAfter().Sub(now) > pki.RenewalWindow {
		return false, nil
	}
	key, csrPEM, err := pki.GenerateNodeKey(m.State.NodeID)
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := orchpb.NewClusterPublicClient(conn).Renew(ctx, &orchpb.RenewRequest{CsrPem: string(csrPEM)})
	if err != nil {
		return false, fmt.Errorf("renew certificate: %w", err)
	}
	if err := m.Install(key, []byte(resp.GetCertPem()), []byte(resp.GetCaBundlePem())); err != nil {
		return false, fmt.Errorf("install renewed certificate: %w", err)
	}
	return true, nil
}

// RunRenewal calls attempt every interval (after a short initial delay) until
// ctx is done, logging failures and trying again next time. With the daily
// interval used by the agent and a 90-day window, a node gets ~90 chances to
// renew before its certificate can lapse.
func RunRenewal(ctx context.Context, initial, interval time.Duration, attempt func(context.Context) (bool, error), logf func(format string, args ...any)) {
	timer := time.NewTimer(initial)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		renewed, err := attempt(ctx)
		switch {
		case err != nil:
			logf("certificate renewal failed (will retry in %s): %v", interval, err)
		case renewed:
			logf("node certificate renewed")
		}
		timer.Reset(interval)
	}
}
