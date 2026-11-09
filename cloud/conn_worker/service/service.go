package service

import (
	"context"
	"net"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/cloud/conn_worker/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
	"github.com/gin-gonic/gin"
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
	// RemoteRelay is the internal HTTP relay nginx proxies remote-access
	// requests to (see remote_relay.go).
	RemoteRelay *RemoteRelay
	config      utils.Config
}

func NewService(config utils.Config) Service {
	repository := repository.NewRepository(config)
	gwReqChan := make(chan StreamHandlerReq)
	connManager, err := NewConnectionManager(repository, gwReqChan, config)
	if err != nil {
		utils.Logger.Error("failed to create connection manager: " + err.Error())
		return nil
	}
	gwServer := NewGatewayServer(gwReqChan)
	relayPort := utils.ConnWorkerRemoteRelayPort
	if config.Connections != nil && config.Connections.RemoteRelayPort != 0 {
		relayPort = config.Connections.RemoteRelayPort
	}
	return &AppService{
		Repository:    repository,
		ConnManager:   connManager,
		GatewayServer: gwServer,
		RemoteRelay:   NewRemoteRelay(connManager, relayPort),
		config:        config,
	}
}

func (s *AppService) NewConnection(ctx context.Context, req types.CreateConnectionRequest) (*types.Connection, error) {
	conn := req.ToConnection()
	if err := s.Repository.Set(ctx, conn.ID, string(conn.ToJSON()), 0); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to create connection: "+err.Error())
		return nil, err
	}
	return conn, nil
}

func (s *AppService) Start(ctx context.Context) error {
	go s.ConnManager.Serve(ctx)
	go s.GatewayServer.Serve()
	if err := s.RemoteRelay.Start(); err != nil {
		utils.Logger.ErrorContext(ctx, "failed to start remote relay", "error", err)
		return err
	}

	router := gin.Default()
	v1 := router.Group(s.config.Http.BasePath.String())
	{
		v1.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{"status": "ok"})
		})
	}
	go func() {
		utils.Logger.Info("conn-worker http server listening", "port", s.config.Http.Port)
		if err := router.Run(":" + s.config.Http.Port.String()); err != nil {
			utils.Logger.Error("http server error", "error", err)
		}
	}()

	return nil
}

func (s *AppService) Stop(ctx context.Context) error {
	var firstErr error
	note := func(what string, err error) {
		if err != nil {
			utils.Logger.ErrorContext(ctx, "failed to stop "+what+": "+err.Error())
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	note("remote relay", s.RemoteRelay.Shutdown(shutdownCtx))
	note("connection manager", s.ConnManager.listener.Close())
	note("gateway server", s.GatewayServer.Close())
	return firstErr
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
	if err := s.Repository.Set(ctx, stream.ID, string(stream.ToJSON()), 0); err != nil {
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
