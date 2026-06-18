package service

import (
	"context"
	"encoding/json"

	"github.com/ambientlabscomputing/underleaf_v2/cloud/conn_worker/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

type Service interface {
	CreateTunnel(ctx context.Context, req types.CreateTunnelRequest) (*types.Tunnel, error)
	GetTunnel(ctx context.Context, id string) (*types.Tunnel, error)
	ListTunnels(ctx context.Context) ([]types.Tunnel, error)
	DeleteTunnel(ctx context.Context, id string) error
	BeginConn(ctx context.Context, req types.BeginConnRequest) (*types.Connection, error)
	TerminateConn(ctx context.Context, req types.TerminateConnRequest) (*types.TerminateConnResponse, error)
}

type AppService struct {
	Repository *repository.Repository
}

func NewService() Service {
	return &AppService{
		Repository: repository.NewRepository(),
	}
}

func (s *AppService) CreateTunnel(ctx context.Context, req types.CreateTunnelRequest) (*types.Tunnel, error) {
	tunnel := req.ToTunnel()
	if err := s.Repository.Set(ctx, tunnel.ID, tunnel, 0); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to create tunnel: "+err.Error())
		return nil, err
	}
	return tunnel, nil
}

func (s *AppService) GetTunnel(ctx context.Context, id string) (*types.Tunnel, error) {
	tunnelStr, err := s.Repository.Get(ctx, id)
	if err != nil {
		utils.Logger.ErrorContext(ctx, "failed to get tunnel: "+err.Error())
		return nil, err
	}
	var tunnel types.Tunnel
	if err := json.Unmarshal([]byte(tunnelStr), &tunnel); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to unmarshal tunnel: "+err.Error())
		return nil, err
	}
	return &tunnel, nil
}

func (s *AppService) ListTunnels(ctx context.Context) ([]types.Tunnel, error) {
	var tunnels []types.Tunnel
	keys, err := s.Repository.List(ctx, "*")
	if err != nil {
		utils.Logger.ErrorContext(ctx, "failed to list tunnels: "+err.Error())
		return nil, err
	}
	for _, key := range keys {
		tunnelStr, err := s.Repository.Get(ctx, key)
		if err != nil {
			utils.Logger.ErrorContext(ctx, "failed to get tunnel during list: "+err.Error())
			continue
		}
		var tunnel types.Tunnel
		if err := json.Unmarshal([]byte(tunnelStr), &tunnel); err != nil {
			utils.Logger.ErrorContext(ctx, "failed to unmarshal tunnel during list: "+err.Error())
			continue
		}
		tunnels = append(tunnels, tunnel)
	}
	return tunnels, nil
}

func (s *AppService) DeleteTunnel(ctx context.Context, id string) error {
	if err := s.Repository.Delete(ctx, id); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to delete tunnel: "+err.Error())
		return err
	}
	return nil
}

func (s *AppService) BeginConn(ctx context.Context, req types.BeginConnRequest) (*types.Connection, error) {
	// Implement the logic to begin a connection here
	return nil, nil
}

func (s *AppService) TerminateConn(ctx context.Context, req types.TerminateConnRequest) (*types.TerminateConnResponse, error) {
	// Implement the logic to terminate a connection here
	return nil, nil
}
