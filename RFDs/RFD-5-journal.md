# RFD-5 Implementation Journal

Base: main @ 782f917 · Integration branch: rfd-5/integration · Last updated: 2026-11-04 by coordinator

## Status

| Step | Title | State | Owner | Branch / PR | Needs | Updated |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | Network isolation | not started | none | none | none | none |
| 2 | Stream port denylist | not started | none | none | none | none |
| 3 | One-origin UI + API | not started | none | none | none | none |
| 4 | Cluster CA, listener, `nodes add`, join | not started | none | none | none | none |
| 5 | Agent link | not started | none | none | 4 | none |
| 6 | Scoped API tokens | not started | none | none | 3 | none |
| 7a | Cloud: split signing key, publish key set | in progress | agent-C | rfd-5/track-c | none | 2026-11-04 |
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
