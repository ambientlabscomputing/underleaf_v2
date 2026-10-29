package service

import (
	"io"
	"net"
	"testing"
)

func TestCountingConnCountsReadsAndWrites(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	counted := newCountingConn(server)
	defer counted.Close()

	payload := []byte("hello, world")
	done := make(chan struct{})
	go func() {
		defer close(done)
		client.Write(payload)
	}()

	buf := make([]byte, len(payload))
	n, err := io.ReadFull(counted, buf)
	<-done
	if err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("expected to read %d bytes, got %d", len(payload), n)
	}
	if got := counted.BytesRead(); got != int64(len(payload)) {
		t.Errorf("BytesRead() = %d, want %d", got, len(payload))
	}
	if got := counted.BytesWritten(); got != 0 {
		t.Errorf("BytesWritten() = %d, want 0 (nothing written yet)", got)
	}

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		io.ReadFull(client, make([]byte, len(payload)))
	}()
	n, err = counted.Write(payload)
	<-readDone
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("expected to write %d bytes, got %d", len(payload), n)
	}
	if got := counted.BytesWritten(); got != int64(len(payload)) {
		t.Errorf("BytesWritten() = %d, want %d", got, len(payload))
	}
}
