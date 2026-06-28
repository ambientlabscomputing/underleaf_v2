package service

import (
	"context"
	"net"

	"github.com/ambientlabscomputing/underleaf_v2/cloud/conn_worker/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

type StreamHandlerReq struct {
	StreamID string
	Stream   net.Conn
	Ctx      context.Context
}

type Service interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	NewConnection(ctx context.Context, req types.CreateConnectionRequest) (*types.Connection, error)
	GetConnection(ctx context.Context, id string) (*types.Connection, error)
	ListConnections(ctx context.Context) ([]types.Connection, error)
	TerminateConnection(ctx context.Context, req types.TerminateConnRequest) (*types.TerminateConnResponse, error)
	NewStream(ctx context.Context, req types.NewStreamRequest) (*types.Stream, error)
	CloseStream(ctx context.Context, req types.CloseStreamRequest) (*types.CloseStreamResponse, error)
	ListStreams(ctx context.Context) ([]types.Stream, error)
	GetStream(ctx context.Context, id string) (*types.Stream, error)
}

type AppService struct {
	Repository    *repository.Repository
	ConnManager   *ConnectionManager
	GatewayServer *GatewayServer
	config        utils.Config
}

func NewService(config utils.Config) Service {
	repository := repository.NewRepository()
	gwReqChan := make(chan StreamHandlerReq)
	connManager, err := NewConnectionManager(repository, gwReqChan, config)
	if err != nil {
		utils.Logger.Error("failed to create connection manager: " + err.Error())
		return nil
	}
	gwServer := NewGatewayServer(gwReqChan)
	return &AppService{
		Repository:    repository,
		ConnManager:   connManager,
		GatewayServer: gwServer,
		config:        config,
	}
}

func (s *AppService) NewConnection(ctx context.Context, req types.CreateConnectionRequest) (*types.Connection, error) {
	conn := req.ToConnection()
	if err := s.Repository.Set(ctx, conn.ID, conn, 0); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to create connection: "+err.Error())
		return nil, err
	}
	return conn, nil
}

func (s *AppService) Start(ctx context.Context) error {
	go s.ConnManager.Serve(ctx)
	go s.GatewayServer.Serve()
	return nil
}

func (s *AppService) Stop(ctx context.Context) error {
	if err := s.ConnManager.listener.Close(); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to stop connection manager: "+err.Error())
		return err
	}
	if err := s.GatewayServer.listener.Close(); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to stop gateway server: "+err.Error())
		return err
	}
	return nil
}

func (s *AppService) GetConnection(ctx context.Context, id string) (*types.Connection, error) {
	connStr, err := s.Repository.Get(ctx, id)
	if err != nil {
		utils.Logger.ErrorContext(ctx, "failed to get connection: "+err.Error())
		return nil, err
	}
	var conn types.Connection
	if err := connStr.Parse(&conn); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to unmarshal connection: "+err.Error())
		return nil, err
	}
	return &conn, nil
}

func (s *AppService) ListConnections(ctx context.Context) ([]types.Connection, error) {
	var conns []types.Connection
	keys, err := s.Repository.List(ctx, string(types.ConnectionIDPrefix)+"_*")
	if err != nil {
		utils.Logger.ErrorContext(ctx, "failed to list connections: "+err.Error())
		return nil, err
	}
	for _, key := range keys {
		connStr, err := s.Repository.Get(ctx, key)
		if err != nil {
			utils.Logger.ErrorContext(ctx, "failed to get connection during list: "+err.Error())
			continue
		}
		var conn types.Connection
		if err := connStr.Parse(&conn); err != nil {
			utils.Logger.ErrorContext(ctx, "failed to unmarshal connection during list: "+err.Error())
			continue
		}
		conns = append(conns, conn)
	}
	return conns, nil
}

func (s *AppService) TerminateConnection(ctx context.Context, req types.TerminateConnRequest) (*types.TerminateConnResponse, error) {
	if err := s.ConnManager.CloseConnection(ctx, req.ConnectionID); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to terminate connection: "+err.Error())
		return nil, err
	}
	if err := s.Repository.Delete(ctx, req.ConnectionID); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to delete connection: "+err.Error())
		return nil, err
	}
	return &types.TerminateConnResponse{
		ID:     req.ConnectionID,
		Status: "succeeded",
	}, nil
}

func (s *AppService) NewStream(ctx context.Context, req types.NewStreamRequest) (*types.Stream, error) {
	stream := req.ToStream()
	if err := s.Repository.Set(ctx, stream.ID, stream, 0); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to create stream: "+err.Error())
		return nil, err
	}
	return stream, nil
}

func (s *AppService) CloseStream(ctx context.Context, req types.CloseStreamRequest) (*types.CloseStreamResponse, error) {
	if err := s.ConnManager.CloseStream(ctx, req.StreamID); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to terminate stream: "+err.Error())
		return nil, err
	}
	return &types.CloseStreamResponse{
		ID:     req.StreamID,
		Status: "succeeded",
	}, nil
}

func (s *AppService) GetStream(ctx context.Context, id string) (*types.Stream, error) {
	streamStr, err := s.Repository.Get(ctx, id)
	if err != nil {
		utils.Logger.ErrorContext(ctx, "failed to get stream: "+err.Error())
		return nil, err
	}
	var stream types.Stream
	if err := streamStr.Parse(&stream); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to unmarshal stream: "+err.Error())
		return nil, err
	}
	return &stream, nil
}

func (s *AppService) ListStreams(ctx context.Context) ([]types.Stream, error) {
	var streams []types.Stream
	keys, err := s.Repository.List(ctx, string(types.StreamIDPrefix)+"_*")
	if err != nil {
		utils.Logger.ErrorContext(ctx, "failed to list streams: "+err.Error())
		return nil, err
	}
	for _, key := range keys {
		streamStr, err := s.Repository.Get(ctx, key)
		if err != nil {
			utils.Logger.ErrorContext(ctx, "failed to get stream during list: "+err.Error())
			continue
		}
		var stream types.Stream
		if err := streamStr.Parse(&stream); err != nil {
			utils.Logger.ErrorContext(ctx, "failed to unmarshal stream during list: "+err.Error())
			continue
		}
		streams = append(streams, stream)
	}
	return streams, nil
}
