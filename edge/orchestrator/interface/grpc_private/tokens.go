package grpc_private

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/service"
)

//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative orchestrator_tokens.proto

// TokensServer mints, lists and revokes API tokens (step 6 of
// RFD 5). It runs on the unix socket only.
type TokensServer struct {
	UnimplementedOrchestratorTokensServer
	Tokens *service.TokenService
}

func tokenInfo(t *repository.APIToken, now time.Time) *TokenInfo {
	info := &TokenInfo{
		Id:            t.ID,
		Name:          t.Name,
		Scope:         t.Scope,
		Status:        service.TokenStatus(t, now),
		CreatedAtUnix: t.CreatedAt.Unix(),
		ExpiresAtUnix: t.ExpiresAt.Unix(),
	}
	if t.RevokedAt != nil {
		info.RevokedAtUnix = t.RevokedAt.Unix()
	}
	if t.LastUsedAt != nil {
		info.LastUsedAtUnix = t.LastUsedAt.Unix()
	}
	return info
}

func (s *TokensServer) CreateToken(_ context.Context, req *CreateTokenRequest) (*CreateTokenResponse, error) {
	scope, err := service.ParseScope(req.GetScope())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	// Bound the seconds before converting: multiplying an arbitrary int64 by
	// time.Second can wrap around into a "valid" duration.
	secs := req.GetTtlSeconds()
	if secs <= 0 || secs > int64(service.MaxTokenTTL/time.Second) {
		return nil, status.Errorf(codes.InvalidArgument, "ttl must be between %s and %s (tokens always expire)", service.MinTokenTTL, service.MaxTokenTTL)
	}
	created, err := s.Tokens.Create(req.GetName(), scope, time.Duration(secs)*time.Second)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &CreateTokenResponse{Token: created.Secret, Info: tokenInfo(created.Info, time.Now())}, nil
}

func (s *TokensServer) ListTokens(context.Context, *ListTokensRequest) (*ListTokensResponse, error) {
	list, err := s.Tokens.List()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	resp := &ListTokensResponse{}
	for _, t := range list {
		resp.Tokens = append(resp.Tokens, tokenInfo(t, now))
	}
	return resp, nil
}

func (s *TokensServer) RevokeToken(_ context.Context, req *RevokeTokenRequest) (*RevokeTokenResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument, "token id required")
	}
	if err := s.Tokens.Revoke(req.GetId()); err != nil {
		if errors.Is(err, repository.ErrAPITokenNotFound) {
			return nil, status.Errorf(codes.NotFound, "no token with id %q", req.GetId())
		}
		return nil, err
	}
	return &RevokeTokenResponse{}, nil
}
