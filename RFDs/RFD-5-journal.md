# RFD-5 Implementation Journal

Base: main @ 782f917 · Integration branch: rfd-5/integration · Last updated: 2026-11-04 by coordinator

## Status

| Step | Title | State | Owner | Branch / PR | Needs | Updated |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | Network isolation | not started | none | none | none | none |
| 2 | Stream port denylist | not started | none | none | none | none |
| 3 | One-origin UI + API | not started | none | none | none | none |
| 4 | Cluster CA, listener, `nodes add`, join | in review | agent-B | rfd-5/track-b | none | 2026-11-04 |
| 5 | Agent link | in progress | agent-B | rfd-5/track-b | 4 | 2026-11-04 |
| 6 | Scoped API tokens | not started | none | none | 3 | none |
| 7a | Cloud: split signing key, publish key set | not started | none | none | none | none |
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
- [x] A second machine/container joins with only the pasted string (second OS process `ufagentd join`, and an alpine-based container via `host.docker.internal`; see log entry "real-process proof")
- [x] Wrong CA fingerprint refuses BEFORE the secret is sent (test: `TestWrongFingerprintRefusesBeforeTheSecretIsSent`, `TestJoinWrongFingerprintNeverSendsSecret` assert the join handler saw 0 requests; real process: manager log shows no join request)
- [x] Reused secret fails; expired secret fails (tests: `TestJoinSecretIsSingleUse`, `TestJoinSecretExpires`, `TestJoinSecretAtExactExpiryBoundaryIsRefused`, over the wire in `TestReusedAndExpiredSecretsFailOverTheWire`; real process: reuse refused, **expiry over the real 15 minutes was not waited for**, only via injected clock)
- [x] Non-mTLS client refused on the cluster listener (tests in `shared/utils/clusterlistener_test.go` and `interface/cluster/server_test.go`; real: openssl alert 116 "certificate required", grpcurl plaintext/no-cert fail)
- [x] `orcli nodes rm` revokes the cert (tests + real: openssl alert 42 "bad certificate" with the removed node's certificate); expiry shown in `orcli nodes ls`, warning at 30 days (`TestListNodesShowsExpiryAndWarnsAt30Days`, `TestCertColumn`; **the 30-day warning was only exercised by unit tests, no real node is that close to expiry**)
- [x] Secrets stored hashed (`TestAddNodeEnablesListenerAndStoresOnlyAHash` greps the sqlite file and WAL for the plaintext in every encoding); common checks green; README Security model updated

### Log

#### 2026-11-04 19:10 · agent-B · cluster CA, cluster listener, join, renewal, revocation, CLI
- Did (commit `ab214ab` plus follow-ups on `rfd-5/track-b`):
  - New package [`shared/pki`](../shared/pki): cluster CA (ECDSA P-256, 10 y, key `0600` in a `0700` dir, refuses to regenerate over a partial CA), node certs (730 d, `NotBefore` -5 min, client-auth only, CN = node id; the CSR's other requested names/usages are ignored), trust bundle that can hold several CAs, join string (`ulj_` + base64url of version|expiry|CA fingerprint|256-bit secret|address), pinned-fingerprint TLS client, node-side membership store, `pki.Join` client.
  - [`shared/utils/clusterlistener.go`](../shared/utils/clusterlistener.go): `ListenCluster` refuses unless the mTLS config has `RequireAndVerifyClientCert` + a CA pool; one port, h2 -> mTLS gRPC, http/1.1 -> join-only HTTP.
  - [`ClusterService`](../edge/orchestrator/service/cluster_service.go) (tokens hashed, consumed atomically, deny list keyed by serial and pruned after expiry), cluster listener [`interface/cluster`](../edge/orchestrator/interface/cluster/server.go) (`ClusterPublic.Renew`, a pinned-to-caller subset of `OrchestratorPublic`, re-checks revocation and membership on every RPC so `nodes rm` also cuts open connections), migration [`0050_cluster_membership`](../edge/orchestrator/repository/migrations/0050_cluster_membership.go) (settings, join_tokens, node_certs, revoked_certs, `nodes.transport`).
  - CLI: `orcli nodes add [--advertise]`, `rm`, `ls` (table with LINK and CERT EXPIRES, warns <= 30 d); new private service in its own proto files (`cluster_private.proto`, `cluster_public.proto`) so generated code does not collide with other tracks.
  - Worker: `ufagentd join <string> [--force]` ([join.go](../edge/agent/cmd/ufagentd/join.go), [membership_service.go](../edge/agent/service/membership_service.go)); daily renewal loop started when the node is a member, 90 days before expiry, over the node's existing mTLS connection.
  - [`install.sh`](../edge/orchestrator_ui/public/tools/install.sh): `join <string> [tag]` subcommand (installs ufagent/ufagentd only, enrolls *before* installing the service, skips Docker). **Only syntax-checked with `sh -n`; never run.**
  - Env overrides so a scratch cluster can run beside a real install: `UNDERLEAF_ORCH_SOCKET`, `UNDERLEAF_AGENT_SOCKET`, `UNDERLEAF_ORCH_LOCAL_PORT`, `UNDERLEAF_ORCH_CLUSTER_PORT`, `UNDERLEAF_CLUSTER_BIND`, `UNDERLEAF_ORCHESTRATOR_ADDR`, `UNDERLEAF_CLUSTER_DIR` ([endpoints.go](../shared/utils/endpoints.go)). Needed because a real `orch-server` was running on this machine on 50100/9090 and `ListenUnixPrivate` would have deleted its live socket.
- Verified:
  - `go build ./...`, `go build -tags devmode ./...`, `go vet ./...` -> clean. `go test -race ./...` -> all `ok` (new: `shared/pki`, `shared/utils` cluster listener + endpoints, `edge/orchestrator/service` cluster, `edge/orchestrator/interface/cluster` end-to-end over a real loopback listener incl. `ufagentd join` logic, `interface/cli/commands/nodes`, `shared/clients` renewal loop). `gofmt -l` -> only `edge/agent/service/docker_service.go` (pre-existing, **not touched**, Track A owns that file).
  - `sh -n edge/orchestrator_ui/public/tools/install.sh` -> OK.
  - **Real-process proof** (script `final4.sh`, hermetic: scratch dirs, sockets in `/tmp/rfd5b-sock`, ports 25900-25902, `orch-server` + `orcli` + two `ufagentd join` processes + one container `rfd5-b-worker`, all cleaned up):
    ```
    == cluster port before nodes add:   (nothing listening)
    == after nodes add:                 orch-serv ... TCP *:25902 (LISTEN)
    == w1: ufagentd join with a tampered CA fingerprint
    Error: could not join 127.0.0.1:25902: ... pki: server certificate chain does not contain the pinned CA (wrong cluster, or the connection is being intercepted)   exit=1
    == join requests the manager had seen before: 0   ... after: 0   (secret never sent)
    == w1: join with the real string:   joined as node node_dblu85o1jmp45mm2de8g; certificate valid until 2028-11-04 / mTLS connection to the manager verified.
    == w2: reuse of the spent string:   Error: the manager rejected the join secret (invalid, already used, or expired) ...  exit=1
    ID  NAME  TRANSPORT  LINK  CERT EXPIRES
    node_dblu85o1jmp45mm2de8g  Joses-MacBook-Pro.local  cluster  down  2028-11-03 (729d)
    grpcurl no-cert / plaintext: Failed to dial ... context deadline exceeded
    openssl h2 no-cert:        ... tlsv13 alert certificate required ... SSL alert number 116
    member Ping with certificate: {"responder": "orchestrator", ...}
    orcli nodes rm: Removed node ... and revoked 1 certificate(s).
    openssl revoked-cert:      ... ssl/tls alert bad certificate ... SSL alert number 42
    re-enroll: re-enrolled (node id kept) as node node_dblu85o1jmp45mm2de8g
    container worker: joined as node node_dblu8abv8f5s72rcdd3g ... ; listening TCP sockets: (none)
    ```
    Manager log: `generated cluster CA sha256:74:c4:...`, `cluster listener on [::]:25902 (mTLS required ...)`, two `joined`, one `join denied`.
- Not verified: the 15-minute secret expiry and the 90-day / 30-day thresholds in real time (injected clocks only); `install.sh join` end to end (never run, per environment rules); a manager installed through the Docker image (see Open issues); IPv6 advertised addresses beyond a parsing test; Linux workers beyond the alpine container's `join` (daemon not started on the worker until step 5); concurrency stress of the join endpoint.
- Next: step 5 (agent link).
- Blocked: none

### Decisions and deviations

All of these are clarifications of an under-specified RFD, not reversals of a settled decision. The first is also amended into the RFD ("Two listeners").
1. **How a node joins on a listener that "requires mTLS".** A joining node has no certificate, so a listener that demanded one at the handshake could not serve the join. Decision: one port, two sides split by ALPN in the TLS handshake. h2/gRPC needs a client cert chaining to the cluster CA; http/1.1 reaches only `POST /v1/join`. `ListenCluster` is the only constructor for a non-loopback listener and refuses mTLS configs that do not require+verify client certs and join configs that could negotiate h2 (tests: `TestListenClusterRefusesWeakTLSConfigs`). Alternatives rejected: optional client cert + per-method interceptor (the listener would not literally "require mTLS"); a second port (a second unauthenticated-ish surface).
2. **Join transport is HTTPS+JSON, renewal is gRPC.** Join happens before any gRPC credential exists; renewal runs over the member's existing mTLS connection (the same `ClientConn` the node uses for everything else, so "over the live mTLS link" holds literally).
3. **Pinning.** Fingerprint = SHA-256 of the CA certificate DER. The server presents leaf + CA; the node requires the chain to contain a CA with that fingerprint and the leaf to verify under *only* that CA with server-auth usage, during the handshake. Hostnames are not checked at join (CA pin is the root of trust, as with kubeadm's discovery hash). After joining, nodes verify the manager against the trust bundle and a fixed name (`orchestrator.cluster.underleaf`) so a manager address change does not break members. Node certs carry client-auth only, so a node cannot impersonate the manager (test).
4. **TLS 1.3 only, ECDSA P-256 keys** (the CSR must carry an ECDSA key). The cloud flow uses RSA-2048 and an unrelated CA; nothing here reads the cloud CA. Side effect: macOS's system `curl` (SecureTransport, TLS 1.2 max) cannot talk to the cluster port; Go clients, grpcurl and openssl can.
5. **Join string format**: `ulj_` + base64url(version | expiry(8) | CA fingerprint(32) | secret(32) | address). Expiry in the string is informational so a node can fail fast without contacting the manager; the manager's stored expiry is authoritative. A wrong, reused, expired or malformed secret all return the same `401 join denied` (no oracle); a malformed CSR with a *valid* secret returns 400 *without burning the secret*.
6. **Re-enrollment supersedes, renewal does not.** Joining with an existing node id keeps the id and revokes all of that node's earlier certificates (the string holder is authorised by the operator; whoever held the old credential must stop working). Renewal leaves the previous certificate valid until it expires (a node that crashed before saving the new one must not be locked out); `nodes rm` revokes every certificate the node has. A join string cannot claim the id of a `local` node.
7. **Worker command is `ufagentd join <string> [--force]`** (ufagentd is what `install.sh` installs on a worker). `--force` is needed to move a node whose certificate is still valid to a *different* cluster (different CA fingerprint); same-cluster re-enrollment needs no flag.
8. **State locations.** Manager CA: `cluster-ca/` beside the orchestrator DB (override: `UNDERLEAF_CLUSTER_DIR/ca`). Worker credentials: `<user config dir>/underleaf/cluster-node/` (`node.pem` = key+cert in one file so a swap is atomic, `ca_bundle.pem`, `cluster.json` written last). Absolute on purpose: `ufagentd join` runs from a shell and `ufagentd run` from a launchd/systemd service with a different working directory, and the agent DB path is cwd-relative.
9. **Migration id `0050_cluster_membership`** (file `0050_cluster_membership.go`): deliberately far from the sequential 0001-0007 so Track A's migration cannot collide; migrations are applied by id, not order. Adds `nodes.transport` (`local` default, `cluster`), `cluster_settings`, `join_tokens`, `node_certs`, `revoked_certs`. `types.Node` gained `Transport`.
10. **Generated code kept out of shared files**: the new RPCs are in their own proto files (`cluster_private.proto` served on the same unix socket, `cluster_public.proto`) so Track A's regeneration of `orchestrator_private.pb.go` cannot conflict. `nodes ls` now uses `ClusterPrivate.ListClusterNodes` (table output; the old `- ID: .., Name: ..` lines are gone; nothing in the repo parsed them).
11. **Cluster listener binds all interfaces by default** (`UNDERLEAF_CLUSTER_BIND`, `UNDERLEAF_ORCH_CLUSTER_PORT`, default `0.0.0.0:50104`); the advertised address is stored and printed. Safe by construction (mTLS-only), as the RFD intends.
12. `ufagentd` and `orcli nodes add/rm` now exit non-zero on failure (needed by `install.sh join`).

### Open issues
- **Docker-installed manager cannot be reached by remote workers yet**: `install.sh` publishes only `127.0.0.1:50100` and `:80`. A manager in the Docker image needs the cluster port published (e.g. `-p <lan-ip>:50104:50104`) and `orcli`'s unix socket is inside the container. I did not touch the `docker run` line (Track A is rewriting it for network isolation). Coordinator decision needed; see Cross-cutting "Open questions".
- `install.sh` URL printed by `orcli nodes add` is a placeholder (`https://<your-install-host>/install.sh`, overridable with `UNDERLEAF_INSTALL_URL`): the repo does not say where the script is published.
- The agent daemon picks up membership at startup; step 5's link supervisor will re-read it on every (re)connect.
- Pruning of the deny list runs daily in the orchestrator process and on demand in tests; no metric/CLI shows its size.

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
- [ ] Cloud signs assertions with a key distinct from the CA key (test)
- [ ] Key set with key IDs (`kid`) is published
- [ ] Rotation with overlap works (test)
- [ ] Open question 'Channel for fetching the assertion key set' checked against the cloud nginx config and recorded

### Log

### Decisions and deviations

### Open issues

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

### Open questions for the human

### Handoffs

#### 2026-11-04 · coordinator · baseline and setup
- Did: created this journal and recorded the baseline; created `rfd-5/integration` from `main`.
- Verified: see Baseline.
- Next: spawn Track A (steps 1,2,3,6), Track B (steps 4,5,8), Track C (step 7a, then 7b once 3 and 6 are merged).
- Blocked: none
