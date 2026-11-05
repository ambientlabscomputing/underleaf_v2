# RFD-5 Implementation Journal

Base: main @ 782f917 · Integration branch: rfd-5/integration · Last updated: 2026-11-04 by agent-C (7a)

## Status

| Step | Title | State | Owner | Branch / PR | Needs | Updated |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | Network isolation | not started | none | none | none | none |
| 2 | Stream port denylist | not started | none | none | none | none |
| 3 | One-origin UI + API | not started | none | none | none | none |
| 4 | Cluster CA, listener, `nodes add`, join | not started | none | none | none | none |
| 5 | Agent link | not started | none | none | 4 | none |
| 6 | Scoped API tokens | not started | none | none | 3 | none |
| 7a | Cloud: split signing key, publish key set | in review | agent-C | rfd-5/track-c | none | 2026-11-04 |
| 7b | Edge: assertions, remote listener, `ufctl connect` | not started | none | none | 3, 6, 7a | none |
| 8 | Retire stopgaps | not started | none | none | 5 | none |

States: not started · in progress · blocked · in review · done

## Baseline

Recorded 2026-11-04 on `rfd-5/integration` (= `main` @ 782f917, the committed security pass "Security pass (#2)"), before any change. Go 1.25.5 darwin/arm64; Docker Desktop 29.5.3 (daemon was stopped and was started for this work).

| Command | Result |
| --- | --- |
| `go build ./...` | pass (rc 0) |
| `go build -tags devmode ./...` | pass (rc 0) |
| `go vet ./...` | pass (rc 0) |
| `go test ./...` | pass. `ok`: cloud/conn_worker/service, edge/orchestrator/interface/rest, edge/orchestrator/repository, edge/orchestrator/service, shared/clients, shared/utils. All other packages: no test files |
| `gofmt -l $(git ls-files '*.go')` | `edge/agent/service/docker_service.go` (the known pre-existing issue; not fixed here. Step 1 touches that file and may fix it in the same change, recording it) |
| Single-node install end to end | **Not run as written.** `install.sh` downloads GitHub release binaries and installs a launchd/systemd agent plus `~/.local/bin` files on the operator's real machine, and pulls `ghcr.io/ambientlabscomputing/underleaf`. Running it from an agent is neither hermetic nor safe. A hermetic local equivalent (local `docker build` of the image, loopback-published ports, agent run from a build output in a scratch dir) is Step 1's job to establish, and any failures it finds are recorded there, not fixed silently. |
| Docker-based checks | Docker daemon was **stopped** at baseline time; started with Docker Desktop for this work. |

Environment notes for all agents:
- A stale worktree exists at `.claude/worktrees/zen-poitras-bed7b5` (branch `claude/zen-poitras-bed7b5`). Ignore it; `grep`/`find` over the repo root will hit its copy of `install.sh`. Exclude `.claude/`.
- Never run the real `install.sh`, `make install*` or anything that writes to `~/.local/bin`, `~/Library/LaunchAgents` or the user's real `~/.underleaf`-style state dirs. Use scratch directories and uniquely named Docker containers/networks/images (prefix `rfd5-`) and clean them up.
- No pushes. Commits go on your step branch only. The coordinator merges steps into `rfd-5/integration`.

## Step 1: Network isolation

**Goal:** see the RFD [Migration](RFD-5.md#migration) and the relevant section for this step.

**Acceptance:**
- [ ] Exposure reproduced from a workload container (`:9090`, `:50100` reachable) BEFORE the fix, output pasted
- [ ] After fix: from a workload, Orchestrator `:9090` and `:50100` unreachable (output pasted)
- [ ] After fix: host agent still reaches the Orchestrator via the loopback-published port
- [ ] Negative-path tests; build/vet/test/gofmt green; README Security model updated; single-node install needs no new manual steps

### Log

### Decisions and deviations

### Open issues

## Step 2: Stream port denylist

**Goal:** see the RFD [Migration](RFD-5.md#migration) and the relevant section for this step.

**Acceptance:**
- [ ] Creating a stream to a control-plane port is rejected at creation (test)
- [ ] Same port is rejected again at forward time in the agent (test)
- [ ] Common checks green; README Security model updated

### Log

### Decisions and deviations

### Open issues

## Step 3: One-origin UI + API

**Goal:** see the RFD [Migration](RFD-5.md#migration) and the relevant section for this step.

**Acceptance:**
- [ ] UI loads from the Orchestrator's single origin with no CORS configuration (evidence)
- [ ] Log WebSockets work over that origin (evidence)
- [ ] Dev-server workflow still works, or its replacement is documented
- [ ] Common checks green; install.sh / Dockerfile / README updated

### Log

### Decisions and deviations

### Open issues

## Step 4: Cluster CA, listener, `nodes add`, join

**Goal:** see the RFD [Migration](RFD-5.md#migration) and the relevant section for this step.

**Acceptance:**
- [ ] A second machine/container joins with only the pasted string
- [ ] Wrong CA fingerprint refuses BEFORE the secret is sent (test)
- [ ] Reused secret fails; expired secret fails (tests)
- [ ] Non-mTLS client refused on the cluster listener (test)
- [ ] `orcli nodes rm` revokes the cert (test); expiry shown in `orcli nodes ls`, warning at 30 days
- [ ] Secrets stored hashed; common checks green; README Security model updated

### Log

### Decisions and deviations

### Open issues

## Step 5: Agent link

**Goal:** see the RFD [Migration](RFD-5.md#migration) and the relevant section for this step.

**Acceptance:**
- [ ] A worker listens on nothing (checked, output pasted)
- [ ] Commands round-trip over the gRPC bidirectional link (test)
- [ ] Link reconnects after a drop (test)
- [ ] Reconciler picks nodes by link; no hardcoded `127.0.0.1:50101` remains (grep pasted)
- [ ] Open question 'Command vocabulary for the agent link' settled and recorded; common checks green

### Log

### Decisions and deviations

### Open issues

## Step 6: Scoped API tokens

**Goal:** see the RFD [Migration](RFD-5.md#migration) and the relevant section for this step.

**Acceptance:**
- [ ] Tokens created, listed, revoked over the socket (test)
- [ ] Each scope (read|deploy|admin) enforced by a test, including negative cases
- [ ] Database holds no plaintext token (test/inspection)
- [ ] WebSocket tickets are single-use (test)
- [ ] Common checks green; README Security model updated

### Log

### Decisions and deviations

### Open issues

## Step 7a: Cloud: split signing key, publish key set

**Goal:** see the RFD [Migration](RFD-5.md#migration) and the relevant section for this step.

**Acceptance:**
- [x] Cloud signs assertions with a key distinct from the CA key (test)
- [x] Key set with key IDs (`kid`) is published
- [x] Rotation with overlap works (test)
- [x] Open question 'Channel for fetching the assertion key set' checked against the cloud nginx config and recorded

### Log

#### 2026-11-04 18:30 · agent-C · setup
- Did: the assigned worktree no longer existed when the coordinator authorized `git checkout -b rfd-5/track-c 0868881`, so I created a fresh worktree `.claude/worktrees/rfd5-track-c` with `git worktree add .claude/worktrees/rfd5-track-c -b rfd-5/track-c 0868881` (see Deviations). Row 7a set to `in progress` in commit d2157a9.
- Verified: `git log --oneline | head -3` → `0868881 RFD-5: add implementation journal with baseline`, `782f917 Security pass (#2)`, `b5468ee adding .vscode`. Python baseline `pytest -q` (cloud_api, main checkout's venv, `PYTHONPATH=src`) → `103 passed, 42 skipped` (the 42 are the DB integration tests; no Postgres here).
- Next: implement.
- Blocked: none

#### 2026-11-04 18:55 · agent-C · assertion issuer, shared verifier, key set endpoint
- Did:
  - New Go package [shared/assertion](../shared/assertion) (stdlib only): claims, strict key-set parser, `Verify`, a small `Signer` for tests.
  - Python issuer [assertion_lib.py](../cloud/cloud_api/src/cloud_api/lib/assertion_lib.py) (Ed25519/EdDSA, kid = RFC 7638 thumbprint, file-backed key store, rotation, overlap), [assertion_service.py](../cloud/cloud_api/src/cloud_api/service/assertion_service.py) (`issue_for_cluster`, ownership check), endpoint [interface/assertion/assertion.py](../cloud/cloud_api/src/cloud_api/interface/assertion/assertion.py) (`GET /api/v2/cloud/assertion-keys`), [rotate_assertion_key.py](../cloud/cloud_api/src/cloud_api/scripts/rotate_assertion_key.py), config `assertion:` ([config.py](../cloud/cloud_api/src/cloud_api/config.py)), singletons in `service_manager.py`, eager creation at startup in `router.py`.
  - Deployment: `docker-compose.yaml` gives cloud_api its own `assertion_keys` volume (not under `./certs`, which nginx mounts), `configs/local/cloud_api.yaml` points at it. README of cloud_api and a ROADMAP note updated.
  - Tests: [shared/assertion/assertion_test.go](../shared/assertion/assertion_test.go), [interop_test.go](../shared/assertion/interop_test.go) (+ `testdata/`), [test_assertion_lib.py](../cloud/cloud_api/tests/lib/test_assertion_lib.py), [test_assertion_service.py](../cloud/cloud_api/tests/service/test_assertion_service.py), [test_assertion_keys.py](../cloud/cloud_api/tests/interface/test_assertion_keys.py).
- Verified:
  - `go test -count=1 -cover ./shared/assertion` → `ok ... coverage: 95.5% of statements`. Named proofs: `TestRotationWithOverlap` (old+new verify during overlap; old key → `ErrKeyExpired` at its `not_after`, → `ErrUnknownKey` after `Prune`; new key unaffected), `TestAlgorithmConfusion` (alg none/None with and without signature, HS256/384/512 keyed with the public key, EdDSA with wrong `typ`, `crit`), `TestRS256TokenFromCAStyleKeyIsNotAnAssertion` (RSA-signed token carrying a real kid → `ErrAlgorithm`), `TestClaimChecks` (expired, wrong audience, missing scope, wrong issuer, future iat, lifetime too long, missing jti/sub/exp), `TestMalformed` (12 malformed shapes), `TestWrongAlgForKeyInKeySet`, `TestKidWithWrongKeyMaterialFailsSignature`, `TestTamperedPayloadFailsSignature`.
  - `PYTHONPATH=src pytest -q` (cloud_api) → `140 passed, 42 skipped` (103 before; 37 new). Named proofs: `test_assertion_key_is_distinct_from_ca_key` (Ed25519 vs the CA's RSA key; kid, public key bytes and key files differ), `test_ca_signed_token_does_not_verify_as_assertion` (RS256 CA-key token, and one forged with a copied assertion kid and `typ`, both rejected by an independent PyJWT-based verifier), `test_assertion_does_not_validate_as_oauth_token` (assertion → `AuthLib.validate_token` raises `InvalidTokenError`), `test_key_set_is_served_unauthenticated_with_kids` (httpx `TestClient`, no auth, `Cache-Control: public, max-age=300`, only public fields), `test_rotation_with_overlap`, automatic and `--compromised` rotation, rotation by another process picked up, key files 0600 / dir 0700, no `.json` carries private material.
  - Cross-language, both directions: `TestInteropWithPythonIssuer` (Go verifies a token issued by the Python `AssertionLib` from the Python-published JWKS; `testdata/python_issued.json`) and `test_this_issuers_verifier_logic_accepts_what_go_signs` (`testdata/go_issued.json`, deterministic fixed-seed throwaway key; `TestGoIssuedVectorIsCurrent` fails if the file is stale).
  - Live end to end (scratch config in the session scratchpad, uvicorn on 127.0.0.1:35080, no Docker): `curl -si .../api/v2/cloud/assertion-keys` → `HTTP/1.1 200 OK`, `cache-control: public, max-age=300`, one OKP/Ed25519 key with `kid`, `not_before`, `not_after`; `ls -la` of the key dir → `-rw-------` `<kid>.pem`/`.json`, dir `drwx------`. A token minted by the Python lib from that key dir verified in Go (`ParseKeySet` + `Verify`) against the served set (`sub=acct_e2e`). Then `python -m cloud_api.scripts.rotate_assertion_key` while the server kept running: the next `curl` showed 2 keys (old with `not_after` = now + 168h, new with +97 days), and the same token still verified against the 2-key set. The temporary Go test used for this was deleted.
  - Channel experiment: generated the real bootstrap CA via `AuthLib.__bootstrap_certs`, served it with `openssl s_server -cert ca.pem -key key.pem`, connected with a Go client whose `RootCAs` held only that cert: `ServerName="localhost"` → `x509: certificate is not valid for any names, but wanted to match localhost`; `ServerName=""` (IP) → `doesn't contain any IP SANs`; with `InsecureSkipVerify` + `VerifyConnection` doing chain-only `Verify` → `OK 200`. `openssl x509 -noout -subject -ext basicConstraints,subjectAltName` on that cert → `CN=Underleaf OAuth Root CA`, `CA:TRUE`, no SAN.
  - `go build ./...` rc 0; `go build -tags devmode ./...` rc 0; `go vet ./...` rc 0; `go test -count=1 ./...` all `ok` (incl. `shared/assertion`); `gofmt -l $(git ls-files '*.go')` → only the pre-existing `edge/agent/service/docker_service.go`; `gofmt -l shared/assertion` clean; `ruff format` + `ruff check` clean on every new/changed Python file (ruff reports 8 pre-existing errors in other files, untouched). `docker compose -f docker-compose.yaml config -q` → no output (valid).
- Next: coordinator merge; step 7b consumes `shared/assertion` (see Handoffs).
- Blocked: none

### Decisions and deviations

- **Algorithm: Ed25519 / JWS `EdDSA`.** Why: small keys and signatures, deterministic signing, no parameter choices to get wrong (no RSA padding, no ECDSA nonce), standard library support in Go (`crypto/ed25519`) so `shared/assertion` has no third-party dependency, and `cryptography` + PyJWT support it on the issuer side. `kid` = RFC 7638 thumbprint, so a kid is bound to its key. The header `typ` is `underleaf-assertion+jwt`, checked by the verifier, so no other JWT the cloud signs can be mistaken for an assertion even if keys were ever shared by mistake.
- **Issuer is Python, verifier is Go.** The brief asked for a package under `shared/` usable by cloud and edge; cloud_api is Python and owns the secrets and the HTTP endpoint, so the issuer lives in cloud_api and `shared/assertion` holds the claim types, key-set parsing, `Verify`, and a Go `Signer` (used for tests and as a second implementation for the interop vectors). Interop is enforced by committed vectors in both directions.
- **Claims:** `iss` (`underleaf-cloud`), `sub` (account id), `aud` (cluster id, a single string), `scopes` (array of `read`/`deploy`/`admin`, matching RFD step 6), `iat`, `exp`, `jti`. `Verify` requires sub/jti/iat/exp, caps `exp-iat` at 5 minutes (default; issuer TTL 60 s, max 300), 5 s leeway, exact scope match (implication between scopes is the caller's policy).
- **Rotation model.** Each key has `not_before` (creation) and `not_after`; the signer uses the newest key. Rotation (automatic after 90 days on next use, or `rotate_assertion_key`) creates the new key first, then caps older keys' `not_after` at now + overlap (168 h default; immediately with `--compromised`). Keys are published until `not_after`, deleted from disk after. A running server notices an out-of-process rotation through the key directory's mtime. The new key is signing and published from the same instant: verifiers that have not refreshed meet an unknown `kid` and use the RFD's bounded refetch (7b). I did not add a "publish before use" delay; recorded as an option under Open issues.
- **Key storage.** On disk in `assertion.keys_dir`, private key as `<kid>.pem` (PKCS#8, 0600; covered by the existing `**/*.pem` ignore) and public metadata as `<kid>.json`, i.e. the same mechanism the CA key uses (file in a mounted dir), but a separate directory and, in compose, a separate volume that nginx does not mount. Nothing is committed.
- **CA key / OAuth JWT key split: NOT done in this step.** Explicitly: only the new dedicated assertion key was implemented. Reasons: (1) the RFD's requirement is that the assertion key not be the CA/JWT key, which the new key satisfies; (2) splitting the JWT key out of the CA key would invalidate every outstanding access and refresh token (30-day refresh tokens, device sessions) at deploy, a user-visible change that needs a human decision on rollout; (3) it would not remove the CA key from the exposed surface: nginx itself uses the CA cert and key as its TLS server cert/key (`nginx.conf` lines 87-88, 135-136, 175-176) and as the client-CA, so the CA key stays in the nginx process regardless. That is the same finding as the channel question, and the real fix (a CA-issued server leaf, CA key kept only in cloud_api, plus edge-side hostname handling) spans nginx and edge client code outside this step. Registered clusters are unaffected by 7a (the CA and their certs are untouched). Recorded under Open issues and the human questions.
- **Channel question answered** (full text now in [RFD-5.md Open questions](RFD-5.md#open-questions)): nginx presents the CA cert itself, so chain-to-`ca_chain.pem` holds trivially but hostname verification fails for lack of a SAN; recommendation is a CA-issued server leaf and a dedicated pinned listener for the key set, with chain-only `VerifyConnection` only as a local-dev interim.
- **No minting endpoint.** The brief put the attach path in 7b, so `AssertionService.issue_for_cluster` is an internal call and a test asserts that `GET /assertion-keys` is the only assertion route. Whoever wires 7b must decide the authorization for the call site (the service already refuses clusters the account does not own, with one error for missing and foreign clusters).
- **RFD amended** (Open questions: channel; Remote access: one sentence on the key split status).
- **Process deviation:** see Cross-cutting, the worktree had to be recreated.

### Open issues

- **Existing CA key and OAuth JWT key are still the same key** (and nginx's TLS key). Not split here; reasons above. For the human: decide on the forced re-login and on the nginx server-leaf change; ROADMAP carries the pointer.
- **Fetch channel not implemented**: needs a CA-issued nginx server leaf, a pinned listener/route, and edge client code (7b and the cloud track). Until then 7b can only pin via chain-only verification.
- **Single-process key store.** Keys live on local disk of one cloud_api process; several replicas would each generate their own and publish different sets. Needs shared storage or a DB table before cloud_api is scaled out.
- **No "publish before use" delay** for newly rotated keys (see Decisions); relies on 7b's unknown-kid refetch. Easy to add (activate a key N minutes after publishing) if the human wants fewer refetches.
- **Not exercised in the Docker stack.** I validated with a live uvicorn process and compose-file syntax only; I did not build the cloud_api image or run the full compose stack (nginx, Postgres). The named volume's first-run permissions (container runs as root) are untested.
- **Edge cases not covered by tests**: coarse directory-mtime granularity on exotic filesystems (rotation by another process is detected via mtime), concurrent rotation by two processes, clock rollback.

## Step 7b: Edge: assertions, remote listener, `ufctl connect`

**Goal:** see the RFD [Migration](RFD-5.md#migration) and the relevant section for this step.

**Acceptance:**
- [ ] Rejected: wrong audience, expired, replayed `jti`, missing scope, unknown `kid`, no key at all (tests)
- [ ] Loopback listener rejects assertions; remote listener rejects anything without one (tests)
- [ ] Cached key set survives a cloud outage (test)
- [ ] Unknown-`kid` refetch bounded to once per minute (test)
- [ ] Common checks green; README Security model updated

### Log

### Decisions and deviations

### Open issues

## Step 8: Retire stopgaps

**Goal:** see the RFD [Migration](RFD-5.md#migration) and the relevant section for this step.

**Acceptance:**
- [ ] `UNDERLEAF_CLUSTER_TOKEN`, `UNDERLEAF_INSECURE_ALLOW_REMOTE` and the Dockerfile flag removed; `grep -rn` finds no references (output pasted)
- [ ] Docs updated; common checks green

### Log

### Decisions and deviations

### Open issues

## Cross-cutting

### Deviations from the RFD

- **(agent-C, 7a)** The assigned worktree (`.claude/worktrees/agent-a3f46e81c5de9f331`, branched from `main` instead of `rfd-5/integration`) disappeared during the session; the coordinator authorized branching `rfd-5/track-c` from `0868881`, so I created a new worktree `.claude/worktrees/rfd5-track-c` for it. Nothing else was touched. 7a also did not split the CA key from the OAuth JWT key (step 7a Decisions).

### Open questions for the human

- **(agent-C, 7a)** Split the cloud's OAuth JWT key from the CA key now (forces re-login for all sessions) or together with the nginx server-leaf change? And which channel should the Orchestrator use to fetch the assertion key set: pinned listener with a CA-issued leaf (recommended) or public TLS? See [RFD-5.md Open questions](RFD-5.md#open-questions) and step 7a Open issues.

### Handoffs

#### 2026-11-04 · agent-C · 7a to 7b
- Did: delivered `shared/assertion` (public API below), the issuer and the key set endpoint `GET /api/v2/cloud/assertion-keys` (JWKS, unauthenticated, `Cache-Control: public, max-age=300`).
- 7b API: `assertion.ParseKeySet([]byte) (*KeySet, error)`; `(*KeySet).Marshal() ([]byte, error)`; `(*KeySet).Lookup(kid, now, leeway) (ed25519.PublicKey, error)`; `(*KeySet).Prune(now) *KeySet`; `assertion.Verify(token string, ks *KeySet, opts VerifyOptions) (*Claims, error)` with `VerifyOptions{Audience, Issuer, RequiredScopes, Now, Leeway, MaxLifetime}`; `Claims{Issuer, Subject, Audience, Scopes, IssuedAt, ExpiresAt, JTI}` with `HasScope`, `Expiry`; sentinel errors `ErrMalformed, ErrAlgorithm, ErrNoKeys, ErrUnknownKey, ErrKeyExpired, ErrBadSignature, ErrIssuer, ErrExpired, ErrNotYetValid, ErrLifetime, ErrAudience, ErrScope` (`errors.Is`). Only `ErrUnknownKey` should trigger a (rate limited) refetch. Test helpers: `NewSigner(kid, ed25519.PrivateKey)`, `(*Signer).Sign(Claims)`, `NewJWK`, `Thumbprint`.
- Things 7b must still do: fetch/cache/persist the set (persist `Marshal()` output, `Prune` before saving, keep last good on parse error), bounded refetch, jti replay cache until `Claims.Expiry()`, the remote listener, and choose the fetch channel (see Open questions). `Verify` is stateless and takes the expected cluster id as `Audience`; an empty audience is an error, never a wildcard.
- Blocked: none

#### 2026-11-04 · coordinator · baseline and setup
- Did: created this journal and recorded the baseline; created `rfd-5/integration` from `main`.
- Verified: see Baseline.
- Next: spawn Track A (steps 1,2,3,6), Track B (steps 4,5,8), Track C (step 7a, then 7b once 3 and 6 are merged).
- Blocked: none
