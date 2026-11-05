// Package cluster serves the Orchestrator's cluster listener (RFD 5, "Two
// listeners"): the one TCP listener that may face a network, protected by mTLS
// against the cluster-local CA. It exists only after `orcli nodes add`.
package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	grpc_public "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/grpc_public"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/pki"
	sharedtypes "github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// Backend is the part of the Orchestrator service the cluster listener needs.
// *service.AppService satisfies it.
type Backend interface {
	Cluster() *service.ClusterService
	Health() *service.HealthService
	Containers() *service.ContainerService
}

// Server manages the cluster listener's lifecycle.
type Server struct {
	Service Backend

	// Register lets other packages (the agent link) add gRPC services to the
	// cluster listener. They are served behind the same mTLS + membership
	// interceptors.
	Register func(*grpc.Server)

	// Bind overrides the listen address (host:port). Empty means
	// UNDERLEAF_CLUSTER_BIND (default 0.0.0.0) and UNDERLEAF_ORCH_CLUSTER_PORT
	// (default 50104).
	Bind string

	joinAttempts atomic.Int64

	mu      sync.Mutex
	lis     *utils.ClusterListener
	grpcSrv *grpc.Server
	httpSrv *http.Server
}

type ctxKey struct{}

// NodeID returns the authenticated node id (the verified client certificate's
// CN) of the caller of a cluster-listener RPC.
func NodeID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKey{}).(string)
	return id, ok
}

// JoinAttempts is how many requests the join endpoint has received (whatever
// their outcome); tests use it to prove a request never arrived.
func (s *Server) JoinAttempts() int64 { return s.joinAttempts.Load() }

// Addr is the bound address, or "" if the listener is not running.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lis == nil {
		return ""
	}
	return s.lis.Addr().String()
}

// Start brings the cluster listener up. It is idempotent. advertise is the
// address nodes will be told to dial; its host becomes a SAN of the server
// certificate.
func (s *Server) Start(advertise string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lis != nil {
		return nil
	}
	cs := s.Service.Cluster()
	mtls, join, err := cs.ServerTLS(advertise)
	if err != nil {
		return err
	}
	bind := s.Bind
	if bind == "" {
		bind = net.JoinHostPort(utils.ClusterBind(), utils.OrchestratorClusterPort())
	}
	lis, err := utils.ListenCluster(bind, mtls, join)
	if err != nil {
		return err
	}

	gs := grpc.NewServer(
		grpc.Creds(utils.ClusterGRPCCredentials()),
		grpc.ChainUnaryInterceptor(s.authUnary),
		grpc.ChainStreamInterceptor(s.authStream),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: 10 * time.Second, PermitWithoutStream: true}),
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: 30 * time.Second, Timeout: 10 * time.Second}),
	)
	grpc_public.RegisterClusterPublicServer(gs, &clusterPublic{svc: s.Service})
	grpc_public.RegisterOrchestratorPublicServer(gs, &memberPublic{svc: s.Service})
	if s.Register != nil {
		s.Register(gs)
	}
	hs := &http.Server{
		Handler:           s.joinHandler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	go gs.Serve(lis.GRPC)
	go hs.Serve(lis.Join)
	s.lis, s.grpcSrv, s.httpSrv = lis, gs, hs
	fmt.Printf("[cluster] cluster listener on %s (mTLS required; advertised as %s)\n", lis.Addr(), advertise)
	return nil
}

// Close stops the listener and every connection on it.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lis == nil {
		return
	}
	s.lis.Close()
	s.grpcSrv.Stop()
	s.httpSrv.Close()
	s.lis, s.grpcSrv, s.httpSrv = nil, nil, nil
}

// identify turns the verified TLS state into a node id and re-checks, on every
// call, that the certificate is not revoked and the node is still a member:
// revocation takes effect on connections that are already open, not just on
// new handshakes.
func (s *Server) identify(ctx context.Context) (context.Context, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no peer")
	}
	ti, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "client certificate required")
	}
	id, ok := pki.PeerNodeID(ti.State)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "client certificate required")
	}
	cs := s.Service.Cluster()
	if cs.IsRevoked(pki.SerialString(ti.State.PeerCertificates[0])) {
		return nil, status.Error(codes.Unauthenticated, "certificate revoked")
	}
	if err := cs.CheckMember(id); err != nil {
		return nil, status.Error(codes.PermissionDenied, "not a member of this cluster")
	}
	return context.WithValue(ctx, ctxKey{}, id), nil
}

func (s *Server) authUnary(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
	ctx, err := s.identify(ctx)
	if err != nil {
		return nil, err
	}
	return h(ctx, req)
}

func (s *Server) authStream(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, h grpc.StreamHandler) error {
	ctx, err := s.identify(ss.Context())
	if err != nil {
		return err
	}
	return h(srv, &ctxStream{ServerStream: ss, ctx: ctx})
}

type ctxStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (c *ctxStream) Context() context.Context { return c.ctx }

// --- ClusterPublic -----------------------------------------------------------

type clusterPublic struct {
	grpc_public.UnimplementedClusterPublicServer
	svc Backend
}

func (c *clusterPublic) Renew(ctx context.Context, req *grpc_public.RenewRequest) (*grpc_public.RenewResponse, error) {
	id, _ := NodeID(ctx)
	res, err := c.svc.Cluster().Renew(id, []byte(req.GetCsrPem()))
	switch {
	case err == nil:
		return &grpc_public.RenewResponse{CertPem: res.CertPEM, CaBundlePem: res.BundlePEM, NotAfterUnix: res.NotAfter.Unix()}, nil
	case errors.Is(err, service.ErrNodeForbidden):
		return nil, status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, service.ErrBadJoin):
		return nil, status.Error(codes.InvalidArgument, err.Error())
	default:
		return nil, status.Error(codes.Internal, "renewal failed")
	}
}

// memberPublic exposes only the OrchestratorPublic methods a member node needs
// on the cluster listener, and pins them to the caller's own node id. CreateNode
// and GetNodes are not served here: members are created by the join flow.
type memberPublic struct {
	grpc_public.UnimplementedOrchestratorPublicServer
	svc Backend
}

func (m *memberPublic) Ping(_ context.Context, req *grpc_public.PingRequest) (*grpc_public.PingResponse, error) {
	r, err := m.svc.Health().Ping(req.Source)
	if err != nil {
		return nil, err
	}
	return &grpc_public.PingResponse{Responder: r.Responder, TimestampUnixMs: r.TimestampUnixMs}, nil
}

func (m *memberPublic) ReportContainers(ctx context.Context, req *grpc_public.ReportContainersRequest) (*grpc_public.ReportContainersResponse, error) {
	id, _ := NodeID(ctx)
	if req.GetNodeId() != id {
		return nil, status.Error(codes.PermissionDenied, "a node may only report its own containers")
	}
	for _, c := range req.GetContainers() {
		if c.GetNodeId() != id {
			return nil, status.Error(codes.PermissionDenied, "a node may only report its own containers")
		}
	}
	containers := make([]*sharedtypes.Container, 0, len(req.GetContainers()))
	for _, c := range req.GetContainers() {
		containers = append(containers, &sharedtypes.Container{
			ContainerSpec: sharedtypes.ContainerSpec{Image: c.GetImage()},
			ID:            c.GetId(),
			DockerID:      c.GetDockerId(),
			Name:          c.GetName(),
			NodeID:        sharedtypes.ForeignKey(c.GetNodeId()),
			Status:        c.GetStatus(),
			Uptime:        c.GetUptime(),
		})
	}
	if err := m.svc.Containers().ReportContainers(id, containers); err != nil {
		return nil, err
	}
	return &grpc_public.ReportContainersResponse{Accepted: int32(len(containers))}, nil
}

// --- join --------------------------------------------------------------------

func (s *Server) joinHandler() http.Handler {
	mux := http.NewServeMux()
	lim := newLimiter(30, time.Minute)
	mux.HandleFunc("POST "+pki.JoinPath, func(w http.ResponseWriter, r *http.Request) {
		remote := r.RemoteAddr
		s.joinAttempts.Add(1)
		if !lim.allow(time.Now()) {
			writeErr(w, http.StatusTooManyRequests, "too many join attempts")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		var req pki.JoinRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "malformed request")
			return
		}
		resp, err := s.Service.Cluster().Join(req)
		switch {
		case err == nil:
			fmt.Printf("[cluster] node %s joined from %s\n", resp.NodeID, remote)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case errors.Is(err, service.ErrJoinDenied):
			fmt.Printf("[cluster] join denied from %s\n", remote)
			writeErr(w, http.StatusUnauthorized, "join denied")
		case errors.Is(err, service.ErrBadJoin):
			writeErr(w, http.StatusBadRequest, err.Error())
		default:
			fmt.Printf("[cluster] join from %s failed: %v\n", remote, err)
			writeErr(w, http.StatusInternalServerError, "internal error")
		}
	})
	return mux
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// limiter is a small fixed-window limiter protecting the unauthenticated join
// endpoint's CPU. It is not what protects the secret (256 random bits do).
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	start  time.Time
	n      int
}

func newLimiter(max int, window time.Duration) *limiter { return &limiter{max: max, window: window} }

func (l *limiter) allow(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.start) >= l.window {
		l.start, l.n = now, 0
	}
	if l.n >= l.max {
		return false
	}
	l.n++
	return true
}
