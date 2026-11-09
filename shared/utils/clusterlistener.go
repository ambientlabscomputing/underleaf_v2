package utils

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc/credentials"
)

// The cluster listener (RFD 5, "Two listeners") is the only Underleaf TCP
// listener allowed to face a network. The invariant it enforces is structural
// rather than a flag: a non-loopback listener cannot be built without mTLS.
//
// One port serves two things, told apart during the TLS handshake by ALPN:
//
//   - h2 (gRPC): the full control surface. The handshake itself requires a client
//     certificate chaining to the cluster CA (tls.RequireAndVerifyClientCert), so
//     a connection that reaches a gRPC handler has already been authenticated.
//   - anything else (http/1.1): the join endpoint, which authenticates the
//     server only because a node that is joining has no certificate yet. It
//     serves nothing but the join handler and never negotiates h2, so it cannot
//     reach gRPC.
//
// A client that omits a certificate and claims h2 fails the handshake; a client
// that does not claim h2 can only ever talk to the join handler.

const clusterHandshakeTimeout = 10 * time.Second

// ClusterListener is the demultiplexed cluster port.
type ClusterListener struct {
	// GRPC yields connections whose mTLS handshake has completed. Serve a
	// gRPC server on it with ClusterGRPCCredentials.
	GRPC net.Listener
	// Join yields completed (server-authenticated) TLS connections for the
	// join HTTP handler.
	Join net.Listener

	raw      net.Listener
	closeMu  sync.Once
	done     chan struct{}
	acceptWG sync.WaitGroup
}

// Addr is the address the cluster port is bound to.
func (l *ClusterListener) Addr() net.Addr { return l.raw.Addr() }

// Close stops accepting and closes both sub-listeners.
func (l *ClusterListener) Close() error {
	var err error
	l.closeMu.Do(func() {
		close(l.done)
		err = l.raw.Close()
	})
	return err
}

// checkClusterTLS verifies the two configurations cannot be combined into an
// unauthenticated control surface.
func checkClusterTLS(mtls, join *tls.Config) error {
	if mtls == nil || join == nil {
		return errors.New("cluster listener needs both an mTLS and a join TLS configuration")
	}
	if mtls.ClientAuth != tls.RequireAndVerifyClientCert {
		return errors.New("refusing to open a cluster listener: the mTLS configuration does not require and verify client certificates")
	}
	if mtls.ClientCAs == nil {
		return errors.New("refusing to open a cluster listener: no CA pool to verify client certificates against")
	}
	if mtls.GetConfigForClient != nil {
		return errors.New("refusing to open a cluster listener: the mTLS configuration must not swap itself per connection")
	}
	if join.ClientAuth != tls.NoClientCert {
		return errors.New("join TLS configuration must not request client certificates")
	}
	if len(join.NextProtos) != 1 || join.NextProtos[0] != "http/1.1" {
		return errors.New("refusing to open a cluster listener: the join configuration may only negotiate http/1.1 (it must never reach gRPC)")
	}
	if join.GetConfigForClient != nil {
		return errors.New("join TLS configuration must not swap itself per connection")
	}
	return nil
}

// ListenCluster binds addr (any interface: the mTLS requirement, not the
// address, is what makes this safe) and returns the demultiplexed listener.
func ListenCluster(addr string, mtls, join *tls.Config) (*ClusterListener, error) {
	if err := checkClusterTLS(mtls, join); err != nil {
		return nil, err
	}
	raw, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	top := &tls.Config{
		MinVersion: tls.VersionTLS13,
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			for _, p := range hello.SupportedProtos {
				if p == "h2" {
					return mtls, nil
				}
			}
			return join, nil
		},
	}
	l := &ClusterListener{
		GRPC: newChanListener(raw.Addr()),
		Join: newChanListener(raw.Addr()),
		raw:  raw,
		done: make(chan struct{}),
	}
	l.acceptWG.Add(1)
	go l.acceptLoop(tls.NewListener(raw, top))
	return l, nil
}

func (l *ClusterListener) acceptLoop(tlsLis net.Listener) {
	defer l.acceptWG.Done()
	grpcL, joinL := l.GRPC.(*chanListener), l.Join.(*chanListener)
	defer grpcL.Close()
	defer joinL.Close()
	for {
		conn, err := tlsLis.Accept()
		if err != nil {
			select {
			case <-l.done:
				return
			default:
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return
		}
		go l.route(conn, grpcL, joinL)
	}
}

func (l *ClusterListener) route(conn net.Conn, grpcL, joinL *chanListener) {
	tc, ok := conn.(*tls.Conn)
	if !ok {
		conn.Close()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), clusterHandshakeTimeout)
	defer cancel()
	if err := tc.HandshakeContext(ctx); err != nil {
		conn.Close()
		return
	}
	cs := tc.ConnectionState()
	target := joinL
	if cs.NegotiatedProtocol == "h2" {
		// Belt and braces: the TLS config already required this.
		if len(cs.VerifiedChains) == 0 {
			conn.Close()
			return
		}
		target = grpcL
	}
	if !target.push(tc, l.done) {
		conn.Close()
	}
}

// chanListener is a net.Listener fed by ClusterListener.
type chanListener struct {
	addr  net.Addr
	conns chan net.Conn
	once  sync.Once
	done  chan struct{}
}

func newChanListener(addr net.Addr) *chanListener {
	return &chanListener{addr: addr, conns: make(chan net.Conn), done: make(chan struct{})}
}

func (c *chanListener) push(conn net.Conn, stop <-chan struct{}) bool {
	select {
	case c.conns <- conn:
		return true
	case <-c.done:
		return false
	case <-stop:
		return false
	case <-time.After(clusterHandshakeTimeout):
		return false
	}
}

func (c *chanListener) Accept() (net.Conn, error) {
	select {
	case conn := <-c.conns:
		return conn, nil
	case <-c.done:
		return nil, net.ErrClosed
	}
}

func (c *chanListener) Close() error {
	c.once.Do(func() { close(c.done) })
	return nil
}

func (c *chanListener) Addr() net.Addr { return c.addr }

// ClusterGRPCCredentials returns gRPC server transport credentials for
// connections that ClusterListener already handshook: it exposes the TLS state
// (and so the verified client certificate) to handlers as credentials.TLSInfo
// without doing a second handshake.
func ClusterGRPCCredentials() credentials.TransportCredentials { return handshaken{} }

type handshaken struct{}

func (handshaken) ServerHandshake(c net.Conn) (net.Conn, credentials.AuthInfo, error) {
	tc, ok := c.(*tls.Conn)
	if !ok {
		return nil, nil, fmt.Errorf("cluster gRPC: connection is not a completed TLS connection")
	}
	cs := tc.ConnectionState()
	if !cs.HandshakeComplete || len(cs.VerifiedChains) == 0 {
		return nil, nil, errors.New("cluster gRPC: client certificate was not verified")
	}
	return tc, credentials.TLSInfo{State: cs, CommonAuthInfo: credentials.CommonAuthInfo{SecurityLevel: credentials.PrivacyAndIntegrity}}, nil
}

func (handshaken) ClientHandshake(context.Context, string, net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return nil, nil, errors.New("cluster gRPC: server-side credentials only")
}

func (handshaken) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "tls", SecurityVersion: "1.3"}
}
func (handshaken) Clone() credentials.TransportCredentials { return handshaken{} }
func (handshaken) OverrideServerName(string) error         { return nil }
