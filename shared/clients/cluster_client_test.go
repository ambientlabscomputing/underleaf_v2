package clients

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunRenewalRetriesAfterFailuresAndStopsOnCancel(t *testing.T) {
	var calls atomic.Int32
	var logged atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunRenewal(ctx, time.Millisecond, 5*time.Millisecond,
			func(context.Context) (bool, error) {
				n := calls.Add(1)
				if n < 3 {
					return false, errors.New("manager unreachable")
				}
				return n == 3, nil
			},
			func(string, ...any) { logged.Add(1) })
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for calls.Load() < 5 {
		select {
		case <-deadline:
			t.Fatalf("only %d attempts: the loop did not keep retrying after failures", calls.Load())
		case <-time.After(2 * time.Millisecond):
		}
	}
	if logged.Load() < 3 { // two failures + one success
		t.Errorf("logged %d messages, want failures and the success reported", logged.Load())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunRenewal did not return after cancel")
	}
}
