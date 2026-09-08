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
	return connectionToProto(conn), nil
}

func (s *ConnWorkerGRPCServer) GetConnection(ctx context.Context, req *GetConnectionRequest) (*Connection, error) {
	conn, err := s.service.GetConnection(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return connectionToProto(conn), nil
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
	for _, conn := range connections {
		if err := stream.Send(connectionToProto(&conn)); err != nil {
			return err
		}
	}
	return nil
}

func (s *ConnWorkerGRPCServer) NewStream(ctx context.Context, req *NewStreamRequest) (*Stream, error) {
	var port *int
	if req.Port != 0 {
		p := int(req.Port)
		port = &p
	}
	req_ := types.NewStreamRequest{
		ConnectionID: req.ConnectionId,
		Type:         types.StreamType(req.Type),
		Port:         port,
	}
	stream, err := s.service.NewStream(ctx, req_)
	if err != nil {
		return nil, err
	}
	return streamToProto(stream), nil
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
	for _, str := range streams {
		if err := stream.Send(streamToProto(&str)); err != nil {
			return err
		}
	}
	return nil
}

func (s *ConnWorkerGRPCServer) GetStream(ctx context.Context, req *GetStreamRequest) (*Stream, error) {
	stream, err := s.service.GetStream(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return streamToProto(stream), nil
}

func connectionToProto(conn *types.Connection) *Connection {
	var closedAt string
	if conn.ClosedAt != nil {
		closedAt = *conn.ClosedAt
	}
	return &Connection{
		Id:        conn.ID,
		NodeId:    string(conn.NodeID),
		Name:      conn.Name,
		State:     string(conn.State),
		Status:    string(conn.Status),
		CreatedAt: conn.CreatedAt,
		ClosedAt:  closedAt,
	}
}

func streamToProto(stream *types.Stream) *Stream {
	var closedAt string
	if stream.ClosedAt != nil {
		closedAt = *stream.ClosedAt
	}
	var port int32
	if stream.Port != nil {
		port = int32(*stream.Port)
	}
	return &Stream{
		Id:           stream.ID,
		ConnectionId: string(stream.ConnectionID),
		Type:         string(stream.Type),
		State:        string(stream.State),
		Status:       string(stream.Status),
		Port:         port,
		CreatedAt:    stream.CreatedAt,
		ClosedAt:     closedAt,
	}
}
