package service

import (
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/lib/conn_client"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// fakeStanding records what the dedicated remote client was asked to do and
// behaves like ConnClient.SetConnection: the same id again changes nothing.
type fakeStanding struct {
	mu      sync.Mutex
	started int
	current string
	history []string // every id that became the standing connection, in order
}

func (f *fakeStanding) Start() { f.mu.Lock(); f.started++; f.mu.Unlock() }
func (f *fakeStanding) SetConnection(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.current == id {
		return false
	}
	f.current = id
	f.history = append(f.history, id)
	return true
}
func (f *fakeStanding) ids() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.history...)
}

// *ConnClient must satisfy what the service needs of the remote client.
var _ StandingConnection = (*conn_client.ConnClient)(nil)

func newRemoteSvc(f *fakeStanding, target, override string) *ConnectionService {
	return (&ConnectionService{}).WithRemote(f, target, override)
}

func TestSetRemoteConnectionIsIdempotentForTheSameID(t *testing.T) {
	f := &fakeStanding{}
	s := newRemoteSvc(f, "127.0.0.1:50110", "")
	for i := 0; i < 3; i++ {
		if err := s.SetRemoteConnection("conn_a"); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.ids(); len(got) != 1 || got[0] != "conn_a" {
		t.Fatalf("connection restarted: %v", got)
	}
}

func TestSetRemoteConnectionSwitchesToANewID(t *testing.T) {
	f := &fakeStanding{}
	s := newRemoteSvc(f, "127.0.0.1:50110", "")
	for _, id := range []string{"conn_a", "conn_b", "conn_b", "conn_a"} {
		if err := s.SetRemoteConnection(id); err != nil {
			t.Fatal(err)
		}
	}
	got := f.ids()
	want := []string{"conn_a", "conn_b", "conn_a"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestSetRemoteConnectionRefusals(t *testing.T) {
	f := &fakeStanding{}
	if err := newRemoteSvc(f, "127.0.0.1:50110", "").SetRemoteConnection("  "); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty id: %v", err)
	}
	// No target: the connection would be open for nothing.
	if err := newRemoteSvc(f, "", "").SetRemoteConnection("conn_a"); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("no target: %v", err)
	}
	// No remote client at all (e.g. a ConnectionService built the old way).
	if err := NewConnectionService(utils.Config{}, nil).SetRemoteConnection("conn_a"); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("no remote client: %v", err)
	}
	if got := f.ids(); len(got) != 0 {
		t.Fatalf("a refused request started %v", got)
	}
}

func TestEnvOverridePinsTheStandingConnection(t *testing.T) {
	f := &fakeStanding{}
	s := newRemoteSvc(f, "127.0.0.1:50110", "conn_manual")
	s.Start()
	if got := f.ids(); len(got) != 1 || got[0] != "conn_manual" {
		t.Fatalf("Start did not open the override: %v", got)
	}
	if f.started != 1 {
		t.Fatalf("remote client started %d times", f.started)
	}
	// The Orchestrator's offer is accepted but does not move it.
	if err := s.SetRemoteConnection("conn_from_orchestrator"); err != nil {
		t.Fatal(err)
	}
	if got := f.ids(); len(got) != 1 {
		t.Fatalf("override was moved: %v", got)
	}
}

func TestStartWithoutOverrideOrTargetOpensNothing(t *testing.T) {
	for _, tc := range []struct{ target, override string }{
		{"127.0.0.1:50110", ""}, // the Orchestrator will say which
		{"", "conn_manual"},     // an override with no target is useless
	} {
		f := &fakeStanding{}
		s := newRemoteSvc(f, tc.target, tc.override)
		s.Start()
		if got := f.ids(); len(got) != 0 {
			t.Fatalf("%+v: opened %v", tc, got)
		}
	}
}
