package grpc_private

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	_ "github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository/migrations"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// startTokensServer serves the tokens service on a real owner-only unix socket,
// exactly as the private server does, and returns a client plus the token
// service (to verify tokens independently of the RPC layer).
func startTokensServer(t *testing.T) (OrchestratorTokensClient, *service.TokenService, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "ut")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	cfg := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(cfg, []byte("db_path: "+filepath.Join(dir, "orch.db")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNDERLEAF_CONFIG", cfg)
	repo, err := repository.NewRepository()
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Start(); err != nil {
		t.Fatal(err)
	}
	svc := service.NewTokenService(repo.APITokens())

	sock := filepath.Join(dir, "o.sock")
	lis, err := utils.ListenUnixPrivate(sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	RegisterOrchestratorTokensServer(srv, &TokensServer{Tokens: svc})
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("unix://"+sock, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return NewOrchestratorTokensClient(conn), svc, sock
}

func TestTokensOverTheSocket(t *testing.T) {
	c, svc, sock := startTokensServer(t)
	ctx := context.Background()

	// The socket is owner-only: that is what authorizes minting.
	if utils.VerifyOwnSocket(sock) != nil {
		t.Fatal("socket must be owned by the current user")
	}
	if fi, _ := os.Stat(sock); fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %v, want 0600", fi.Mode().Perm())
	}

	// Create
	created, err := c.CreateToken(ctx, &CreateTokenRequest{Name: "ci", Scope: "deploy", TtlSeconds: int64((24 * time.Hour).Seconds())})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.GetToken(), "ulat_") || created.GetInfo().GetScope() != "deploy" || created.GetInfo().GetStatus() != "active" {
		t.Fatalf("create: %v", created)
	}
	p, err := svc.Authenticate(ctx, created.GetToken())
	if err != nil || p.Scope != service.ScopeDeploy {
		t.Fatalf("minted token must authenticate: %+v %v", p, err)
	}

	// List: shows it, never the secret.
	list, err := c.ListTokens(ctx, &ListTokensRequest{})
	if err != nil || len(list.GetTokens()) != 1 || list.GetTokens()[0].GetId() != created.GetInfo().GetId() {
		t.Fatalf("list: %v %v", list, err)
	}
	raw, _ := proto.Marshal(list)
	secret := strings.SplitN(strings.TrimPrefix(created.GetToken(), "ulat_"), "_", 2)[1]
	if strings.Contains(string(raw), secret) {
		t.Fatal("the listing must never contain the secret")
	}
	if list.GetTokens()[0].GetLastUsedAtUnix() == 0 {
		t.Fatal("authenticating should have recorded last use")
	}

	// Revoke
	if _, err := c.RevokeToken(ctx, &RevokeTokenRequest{Id: created.GetInfo().GetId()}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, created.GetToken()); err == nil {
		t.Fatal("revoked token must no longer authenticate")
	}
	list, _ = c.ListTokens(ctx, &ListTokensRequest{})
	if list.GetTokens()[0].GetStatus() != "revoked" || list.GetTokens()[0].GetRevokedAtUnix() == 0 {
		t.Fatalf("list after revoke: %v", list)
	}
}

func TestTokenRPCNegativePaths(t *testing.T) {
	c, _, _ := startTokensServer(t)
	ctx := context.Background()
	day := int64((24 * time.Hour).Seconds())

	for name, req := range map[string]*CreateTokenRequest{
		"unknown scope":   {Scope: "root", TtlSeconds: day},
		"empty scope":     {Scope: "", TtlSeconds: day},
		"no ttl":          {Scope: "read"},
		"negative ttl":    {Scope: "read", TtlSeconds: -5},
		"ttl too short":   {Scope: "read", TtlSeconds: 5},
		"ttl too long":    {Scope: "read", TtlSeconds: int64(service.MaxTokenTTL.Seconds()) + 3600},
		"overflowing ttl": {Scope: "read", TtlSeconds: 1 << 62},
		// (3600 + 2^55) * 1e9 wraps mod 2^64 to exactly one hour: must not sneak in.
		"wrapping ttl": {Scope: "read", TtlSeconds: 3600 + 1<<55},
		"long name":    {Scope: "read", TtlSeconds: day, Name: strings.Repeat("n", 100)},
	} {
		if _, err := c.CreateToken(ctx, req); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("%s: want InvalidArgument, got %v", name, err)
		}
	}
	if _, err := c.RevokeToken(ctx, &RevokeTokenRequest{Id: "nosuchtoken0"}); status.Code(err) != codes.NotFound {
		t.Fatalf("revoke unknown: want NotFound, got %v", err)
	}
	if _, err := c.RevokeToken(ctx, &RevokeTokenRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("revoke without id: want InvalidArgument, got %v", err)
	}
	// Nothing was created by the failed requests.
	if l, _ := c.ListTokens(ctx, &ListTokensRequest{}); len(l.GetTokens()) != 0 {
		t.Fatalf("failed creates must not leave tokens: %v", l)
	}
}
