package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/interface/rest"
	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/remote"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// assertionKeysFile is where the cloud's assertion key set is persisted, in the
// cert directory next to the CA chain it is fetched over.
const assertionKeysFile = "assertion_keys.json"

// startRemoteAccess turns on the remote listener (RFD 5 step 7b) when
// UNDERLEAF_REMOTE_ADDR is set, and does nothing at all otherwise: no key
// fetches, no extra listener. It wires the assertion verifier to a key cache
// that fetches the cloud's key set over a connection pinned to the cluster's
// ca_chain.pem, persists it, and refreshes it for the life of the process.
func startRemoteAccess(ctx context.Context, srv *rest.OrchestratorRESTServer, cfg utils.Config) {
	if os.Getenv(rest.EnvRemoteAddr) == "" {
		return
	}
	logf := func(format string, args ...any) { fmt.Printf("[remote] "+format+"\n", args...) }

	cache, err := remote.NewKeyCache(remote.KeyCacheConfig{
		Path: filepath.Join(cfg.CertDir, assertionKeysFile),
		Logf: logf,
		Fetch: func(ctx context.Context) ([]byte, error) {
			// Built per fetch so a renewed or newly registered CA chain is used.
			cc, err := clients.NewPinnedCloudClient(cfg)
			if err != nil {
				return nil, fmt.Errorf("cluster not registered with the cloud yet: %w", err)
			}
			return cc.FetchAssertionKeys(ctx)
		},
	})
	if err != nil {
		panic("remote access: " + err.Error())
	}
	verifier, err := remote.NewVerifier(remote.VerifierConfig{
		Keys:     cache,
		Audience: remote.ClusterIDProvider(cfg.CertDir, time.Minute, nil),
	})
	if err != nil {
		panic("remote access: " + err.Error())
	}
	srv.Remote = verifier

	go cache.Run(ctx)
	go func() {
		if err := srv.ServeRemote(); err != nil {
			logf("remote listener stopped: %v", err)
		}
	}()
}
