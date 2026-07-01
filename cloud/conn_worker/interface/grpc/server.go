package grpc

import (
	"context"
	"net"
	"strconv"

	"google.golang.org/grpc"
	emptypb "google.golang.org/protobuf/types/known/emptypb"

	"github.com/ambientlabscomputing/underleaf_v2/cloud/conn_worker/service"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

type ConnWorkerGRPCServer struct {
	UnimplementedConnectionWorkerServer
	service service.Service
	config  utils.Config
}

func NewConnWorkerGRPCServer(service service.Service, config utils.Config) *ConnWorkerGRPCServer {
	return &ConnWorkerGRPCServer{
		service: service,
		config:  config,
	}
}

func (s *ConnWorkerGRPCServer) Serve() {
	lis, _ := net.Listen("tcp", ":"+strconv.Itoa(s.config.GRPC.Port.Int()))
	grpcServer := grpc.NewServer()
	RegisterConnectionWorkerServer(grpcServer, s)
	utils.Logger.Info("conn-worker gRPC server listening", "port", s.config.GRPC.Port)
	grpcServer.Serve(lis)
}

func (s *ConnWorkerGRPCServer) CreateConnection(ctx context.Context, req *CreateConnectionRequest) (*Connection, error) {
	req_ := types.CreateConnectionRequest{
		NodeID: req.NodeId,
		Name:   req.Name,
	}
	conn, err := s.service.NewConnection(ctx, req_)
	if err != nil {
		return nil, err
	}
	return &Connection{
		NodeId: string(conn.NodeID),
		Name:   conn.Name,
	}, nil
}

func (s *ConnWorkerGRPCServer) GetConnection(ctx context.Context, req *GetConnectionRequest) (*Connection, error) {
	conn, err := s.service.GetConnection(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return &Connection{
		NodeId: string(conn.NodeID),
		Name:   conn.Name,
	}, nil
}

func (s *ConnWorkerGRPCServer) TerminateConnection(ctx context.Context, req *TerminateConnectionRequest) (*emptypb.Empty, error) {
	req_ := types.TerminateConnRequest{
		ConnectionID: req.Id,
	}
	_, err := s.service.TerminateConnection(ctx, req_)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *ConnWorkerGRPCServer) ListConnections(req *emptypb.Empty, stream ConnectionWorker_ListConnectionsServer) error {
	connections, err := s.service.ListConnections(stream.Context())
	if err != nil {
		return err
	}
	var grpcConnections []*Connection
	for _, conn := range connections {
		grpcConnections = append(grpcConnections, &Connection{
			NodeId: string(conn.NodeID),
			Name:   conn.Name,
		})
	}
	for _, conn := range grpcConnections {
		stream.Send(conn)
	}
	return nil
}

func (s *ConnWorkerGRPCServer) NewStream(ctx context.Context, req *NewStreamRequest) (*Stream, error) {
	req_ := types.NewStreamRequest{
		ConnectionID: req.ConnectionId,
	}
	stream, err := s.service.NewStream(ctx, req_)
	if err != nil {
		return nil, err
	}
	return &Stream{
		Id: stream.ID,
	}, nil
}

func (s *ConnWorkerGRPCServer) CloseStream(ctx context.Context, req *CloseStreamRequest) (*CloseStreamResponse, error) {
	req_ := types.CloseStreamRequest{
		StreamID: req.StreamId,
	}
	resp, err := s.service.CloseStream(ctx, req_)
	if err != nil {
		return nil, err
	}
	return &CloseStreamResponse{
		Id:     resp.ID,
		Status: resp.Status,
	}, nil
}

func (s *ConnWorkerGRPCServer) ListStreams(req *emptypb.Empty, stream ConnectionWorker_ListStreamsServer) error {
	streams, err := s.service.ListStreams(stream.Context())
	if err != nil {
		return err
	}
	var grpcStreams []*Stream
	for _, str := range streams {
		grpcStreams = append(grpcStreams, &Stream{
			Id: str.ID,
		})
	}
	for _, str := range grpcStreams {
		stream.Send(str)
	}
	return nil
}

func (s *ConnWorkerGRPCServer) GetStream(ctx context.Context, req *GetStreamRequest) (*Stream, error) {
	stream, err := s.service.GetStream(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return &Stream{
		Id: stream.ID,
	}, nil
}
