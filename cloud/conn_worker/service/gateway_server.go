package service

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"

	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

type StreamID string
type GatewayStreams map[StreamID]net.Conn

// GatewayServer is a TCP server that listens for incoming connections and routes them to the appropriate stream based on the stream ID.
// listens for incoming connections on a specified port and routes them to the appropriate stream based on host.
type GatewayServer struct {
	streams  GatewayStreams
	config   utils.Config
	listener net.Listener
	reqChan  chan StreamHandlerReq
}

func NewGatewayServer(reqChan chan StreamHandlerReq) *GatewayServer {
	return &GatewayServer{
		streams:  make(GatewayStreams),
		config:   utils.GetConfig(utils.ConnWorkerConfig),
		listener: nil,
		reqChan:  reqChan,
	}
}

func (gs *GatewayServer) Serve() error {
	go gs.HandleConnectionReqs()

	utils.Logger.Debug("serving Gateway Server ...", "port", gs.config.Connections.GatewayPort, "domain", gs.config.Connections.Domain)
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(gs.config.Connections.GatewayPort))
	if err != nil {
		utils.Logger.Error("failed to start gateway server", "error", err)
		return err
	}
	gs.listener = listener
	defer gs.listener.Close()

	utils.Logger.Info("Gateway server started on port " + strconv.Itoa(gs.config.Connections.GatewayPort))

	for {
		conn, err := gs.listener.Accept()
		if err != nil {
			utils.Logger.Error("failed to accept connection", "error", err)
			continue
		}

		ctx := context.Background()
		ctx = context.WithValue(ctx, "remote_addr", conn.RemoteAddr().String())
		reqID := types.GenerateID(types.RequestIDPrefix)
		ctx = utils.ContextWithLogger(ctx, utils.Logger, &reqID)
		go gs.handleConnection(ctx, conn)
	}
}

func (gs *GatewayServer) HandleConnectionReqs() {
	for req := range gs.reqChan {
		gs.AddStream(StreamID(req.StreamID), req.Stream)
		utils.Logger.Info("Added stream to gateway server", "stream_id", req.StreamID)
	}
}

func (gs *GatewayServer) handleConnection(ctx context.Context, conn net.Conn) {
	logger := utils.LoggerFromContext(ctx)
	reader := bufio.NewReader(conn)
	httpReq, err := http.ReadRequest(reader)
	if err != nil {
		logger.Error("failed to read HTTP request", "error", err)
		conn.Close()
		return
	}

	host := httpReq.Host // <stream_id>.gw.<config.domain>
	streamID := parseIDFromHost(host, ".gw."+gs.config.Connections.Domain)

	if err := gs.BindIncomingConnToStream(ctx, StreamID(streamID), conn); err != nil {
		logger.Error("failed to bind incoming connection to stream", "error", err)
		conn.Close()
		return
	}

	logger.Info("Bound incoming connection to stream", "stream_id", streamID)
}

func (gs *GatewayServer) AddStream(streamID StreamID, conn net.Conn) {
	gs.streams[streamID] = conn
}

func (gs *GatewayServer) RemoveStream(streamID StreamID) {
	if conn, exists := gs.streams[streamID]; exists {
		conn.Close()
		delete(gs.streams, streamID)
	}
}

func (gs *GatewayServer) GetStream(streamID StreamID) (net.Conn, bool) {
	conn, exists := gs.streams[streamID]
	return conn, exists
}

func (gs *GatewayServer) ListStreams() []StreamID {
	streamIDs := make([]StreamID, 0, len(gs.streams))
	for streamID := range gs.streams {
		streamIDs = append(streamIDs, streamID)
	}
	return streamIDs
}

func (gs *GatewayServer) CloseAllStreams() {
	for streamID, conn := range gs.streams {
		conn.Close()
		delete(gs.streams, streamID)
	}
}

func (gs *GatewayServer) BindIncomingConnToStream(ctx context.Context, streamID StreamID, incomingConn net.Conn) error {
	logger := utils.LoggerFromContext(ctx)
	streamConn, exists := gs.streams[streamID]
	if !exists {
		logger.Error("stream not found for streamID: " + string(streamID))
		return fmt.Errorf("stream not found for streamID: %s", streamID)
	}

	go bridge(ctx, incomingConn, streamConn)
	return nil
}

// bridge continually copies data between the incoming connection and the stream connection until one of them is closed.
func bridge(ctx context.Context, incommingConn net.Conn, streamConn net.Conn) {
	logger := utils.LoggerFromContext(ctx)
	defer incommingConn.Close()
	defer streamConn.Close()

	go func() {
		_, err := io.Copy(streamConn, incommingConn)
		if err != nil {
			logger.Error("error while copying from incoming to stream", "error", err)
		}
	}()

	_, err := io.Copy(incommingConn, streamConn)
	if err != nil {
		logger.Error("error while copying from stream to incoming", "error", err)
	}
}
