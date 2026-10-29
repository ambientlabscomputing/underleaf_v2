package service

import (
	"net"
	"sync/atomic"
	"time"
)

// countingConn wraps a net.Conn and atomically counts bytes read/written
// through it, so the gateway's bridge() (which just calls io.Copy on the
// plain net.Conn interface) transparently produces byte-transfer metering
// with no changes to the copy loop itself.
//
// Read = bytes flowing out of the tunnel back to the external client.
// Write = bytes flowing from the external client into the tunnel.
// Which physical direction each maps to depends on which side of the pipe
// this wraps; conn_worker treats the sum as "total traffic" for billing
// purposes and doesn't need the distinction to be semantically precise.
type countingConn struct {
	net.Conn
	bytesRead    atomic.Int64
	bytesWritten atomic.Int64
}

func newCountingConn(conn net.Conn) *countingConn {
	return &countingConn{Conn: conn}
}

func (c *countingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.bytesRead.Add(int64(n))
	}
	return n, err
}

func (c *countingConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if n > 0 {
		c.bytesWritten.Add(int64(n))
	}
	return n, err
}

func (c *countingConn) BytesRead() int64 {
	return c.bytesRead.Load()
}

func (c *countingConn) BytesWritten() int64 {
	return c.bytesWritten.Load()
}

// flushIntervalSeconds is how often periodicFlushStreamBytes persists
// still-active streams' running byte counts into Redis, bounding how much
// usage is lost if conn_worker crashes mid-stream (the alternative --
// relying solely on CloseStream's flush -- loses everything since the last
// successful cloud_api poll for streams that never close cleanly).
const flushIntervalSeconds = 30

// closedRecordFlushGrace extends how long a *closed* stream's Redis record
// survives beyond the configured TTL, specifically so cloud_api's poll loop
// has a bounded-but-real window to read final byte counts even if it's
// stalled (deploy, crash-loop) at the moment the stream closes. This is a
// stopgap, not a fix: if cloud_api is down longer than TTL+grace, usage
// from streams closed in that window is still lost -- see track 5 plan §2.
const closedRecordFlushGrace = 5 * time.Minute
