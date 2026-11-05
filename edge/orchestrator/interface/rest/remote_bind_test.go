package rest

import (
	"strings"
	"testing"
)

// The remote listener is plain HTTP authenticated by an assertion, and its only
// client is the agent's tunnel on the same host, so it follows the local
// listeners' bind policy (RFD 5 step 8): loopback, or the container interface
// inside the Docker image. Nothing else, and none of the retired switches, can
// widen it.
func TestRemoteListenerRefusesNonLoopbackBinds(t *testing.T) {
	e := newRemoteEnv(t, false)
	t.Setenv("UNDERLEAF_CONTAINER", "")
	t.Setenv("UNDERLEAF_INSECURE_ALLOW_REMOTE", "1")
	t.Setenv("UNDERLEAF_CLUSTER_TOKEN", "x")
	for _, addr := range []string{"0.0.0.0:0", ":0", "192.0.2.10:0", "manager.example.com:0"} {
		t.Setenv(EnvRemoteAddr, addr)
		err := e.srv.ServeRemote()
		if err == nil || !strings.Contains(err.Error(), "refusing to bind") {
			t.Errorf("%s: ServeRemote err = %v, want a bind refusal", addr, err)
		}
	}
}
