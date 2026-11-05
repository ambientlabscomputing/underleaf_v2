# RFD-5 Implementation Journal

Base: main @ 782f917 · Integration branch: rfd-5/integration · Last updated: 2026-11-04 by coordinator

## Status

| Step | Title | State | Owner | Branch / PR | Needs | Updated |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | Network isolation | in review | agent-A | rfd-5/track-a | none | 2026-11-04 |
| 2 | Stream port denylist | not started | none | none | none | none |
| 3 | One-origin UI + API | not started | none | none | none | none |
| 4 | Cluster CA, listener, `nodes add`, join | not started | none | none | none | none |
| 5 | Agent link | not started | none | none | 4 | none |
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
- [x] Exposure reproduced from a workload container (`:9090`, `:50100` reachable) BEFORE the fix, output pasted
- [ ] After fix: from a workload, Orchestrator `:9090` and `:50100` unreachable (output pasted) **PARTIAL: `:9090` and DNS blocked, published `:50100`/`:80` still reachable on Docker 29.5.3; see log and Open issues**
- [x] After fix: host agent still reaches the Orchestrator via the loopback-published port
- [x] Negative-path tests; build/vet/test/gofmt green; README Security model updated; single-node install needs no new manual steps (install.sh does it; verified as the extracted block only)

### Log

#### 2026-11-04 · agent-A · reproduce the exposure (BEFORE)
- Did: built the Orchestrator image locally from the baseline tree in a scratch context outside the repo (`CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o ctx/bin/orch-server-linux-arm64 ./orchestrator/cmd/server`, then `docker build -t rfd5-a-orch:before -f docker/Dockerfile ctx`). Note: the Dockerfile's `npm run build` (`tsc -b && vite build`) **fails at baseline** with two TypeScript errors (`PageShell.tsx:116` `primaryTypographyProps`, `theme.ts:102` `containedPrimary`, both MUI 9 typings), so the scratch Dockerfile used `npx vite build` instead (see Open issues). Ran it the way install.sh does: `docker run -d -p 127.0.0.1:15090:80 -p 127.0.0.1:15100:50100 --name rfd5-a-orch-before rfd5-a-orch:before` (default bridge). Inside, `netstat` shows `0.0.0.0:80` (nginx, static UI only), `:::9090` (Orchestrator REST), `:::50100` (agent gRPC). Then a stand-in workload: `docker run -d --name rfd5-a-workload-before alpine:3.20 sleep 600` (default bridge, which is exactly where the agent's `ContainerCreate` puts workloads today), with a throwaway Go gRPC probe copied in.
- Verified (from the workload container, orchestrator at 172.17.0.2, workload at 172.17.0.3):
  ```
  REST :9090  wget --header 'Host: localhost' http://172.17.0.2:9090/api/v1/health -> {"status":"OK"}
              .../api/v1/nodes -> {"results":null,"count":0,"total":0,...}
              POST .../api/v1/deployments {"source":"gh:x/y"} -> HTTP 400 (reached the handler; guards passed)
  gRPC :50100 /probe 172.17.0.2:50100 -> grpc GetNodes OK: 0 node(s), total=0
                                          grpc Ping: responder:"orchestrator" ... <nil>
  ```
  So both `:9090` and `:50100` are reachable from a workload, and the Host guard does not help (the attacker sets `Host: localhost`). Host-side baseline: `127.0.0.1:15100` gRPC OK, `127.0.0.1:15090` -> nginx 200.
- Also found (baseline, not caused by this work): in the Docker install only `:80` (nginx, static files) is published (as host `:9090`); the Orchestrator REST `:9090` inside the container is **not** published. `curl http://127.0.0.1:15090/api/v1/health` returns the SPA `index.html` (200, text/html) because nginx's `try_files` falls back to it. The UI's API base is `http://localhost:9090/api/v1`, so the installed UI cannot reach the API. Step 3 removes this by construction.
- Next: implement the network split in the agent and install.sh.
- Blocked: none

#### 2026-11-04 · agent-A · network isolation implemented, AFTER evidence
- Did: [edge/agent/service/workload_network.go](../edge/agent/service/workload_network.go) (new): the agent creates (idempotently, bridge driver, labelled) a user-defined network `underleaf-workloads` (`UNDERLEAF_WORKLOAD_NETWORK` overrides; `bridge`, `host`, `none`, `default`, `container:*` and the Orchestrator's `underleaf-control` are refused) and `CreateContainer` in [docker_service.go](../edge/agent/service/docker_service.go) now sets `HostConfig.NetworkMode` to it via `workloadHostConfig`. An existing network of that name that is not a bridge is refused. `install.sh` ([diff](../edge/orchestrator_ui/public/tools/install.sh)) creates network `underleaf-control` and runs the Orchestrator container on it; for an existing install it moves the container off the default bridge in place (connect, disconnect `bridge`, `docker restart`; data preserved). `make docker-run` likewise. Also gofmt-fixed `docker_service.go` (the known baseline issue: one const alignment line). README Security model: new "Network isolation" row including the limit below.
- Verified:
  - Unit tests, [workload_network_test.go](../edge/agent/service/workload_network_test.go), all against a fake Docker network API: creates a non-internal bridge when absent; reuses an existing bridge (no recreate); refuses host/null/overlay/macvlan drivers; surfaces inspect and create errors; tolerates a creation race; rejects `bridge`/`host`/`none`/`default`/control-network/`container:x` names (and never touches Docker for them); success cached, failure not cached; `workloadHostConfig` is never default/bridge/host and keeps tmpfs/restart/binds. `go test ./edge/agent/service` -> ok.
  - Real Docker (opt-in): `UNDERLEAF_DOCKER_INTEGRATION=1 UNDERLEAF_WORKLOAD_NETWORK=rfd5-a-workloads go test -run Integration -v ./edge/agent/service` -> PASS: the container created by `DockerService.CreateContainer` is on exactly one network, `rfd5-a-workloads`, not `bridge`; the network is a labelled bridge.
  - install.sh block, executed for real (the `ORCH_NETWORK=`..`fi` block of the actual file, extracted and retargeted to `rfd5-a-*` names and ports 15191/15101): fresh run created `rfd5-a-control` and the container on it with `127.0.0.1:15191->80, 127.0.0.1:15101->50100`. Existing-install path: a legacy container started the old way on `bridge` was moved with the block -> networks `rfd5-a-control` only, host gRPC probe OK, UI 200, container file marker preserved. (Found on the way: without the `docker restart`, the moved container's published ports stop working; the restart is in the script.)
  - **After, from a workload on `rfd5-a-workloads` (172.25.0.2) to the Orchestrator on `rfd5-a-control` (172.24.0.2):**
    ```
    REST :9090 by IP        -> wget: download timed out            (BLOCKED)
    by name rfd5-a-orch     -> wget: bad address                   (BLOCKED, no cross-network DNS)
    nc :9090                -> Operation timed out                 (BLOCKED)
    nc :80                  -> 172.24.0.2:80 open                  (REACHABLE: published port)
    nc :50100 / gRPC probe  -> open / "grpc GetNodes OK: 0 node(s)" (REACHABLE: published port)
    from a container ON rfd5-a-control, REST :9090 -> {"status":"OK"}  (orchestrator itself is healthy)
    ```
    Host path (what the agent uses): `probe-darwin 127.0.0.1:15101` -> `grpc GetNodes OK`; `curl 127.0.0.1:15191/` -> 200.
  - **The isolation is only partial.** Docker 29.5.3 (Desktop, arm64) lets a container on another bridge reach a *published* port through the container's IP, and `host.docker.internal:<published port>` also reaches host-loopback-published ports (verified with a source-address echo server: `nc -z host.docker.internal 15300` from the workload succeeded). Source-IP guards cannot help: all three paths (host, other-network workload, host.docker.internal) arrive at the container with source = the bridge gateway (172.24.0.1), because Docker masquerades the cross-bridge hop. Network options `enable_icc=false` and `gateway_mode_ipv4=nat` were tried; neither blocks it. See Decisions/Open issues and Cross-cutting.
  - `go build ./...`, `go build -tags devmode ./...`, `go vet ./...`, `go test ./...` all pass; `gofmt -l edge shared` empty.
- Next: step 2.
- Blocked: none

### Decisions and deviations

- **Network names**: `underleaf-control` (Orchestrator, created by install.sh/make) and `underleaf-workloads` (workloads, created by the agent on first deploy; override with `UNDERLEAF_WORKLOAD_NETWORK`, which also lets tests use scratch names). The agent owns the workload network because only it knows when a workload is created, and so install.sh has nothing to do for the worker side.
- **Existing installs**: install.sh moves a running `underleaf` container in place instead of failing on the name clash as it did before. Already-deployed workloads stay on the default bridge until their next (re)deploy, because `CreateContainer` force-removes and recreates; no automatic migration of running workloads. Deliberate: avoids restarting user services during an install.
- **Deviation (needs human decision, see Cross-cutting): the RFD's premise "separate user-defined networks are isolated from each other" does not hold for published ports on the Docker used here.** Unpublished ports (REST `:9090` today) and DNS are isolated; published ones are not. Because the single-node design publishes `:50100` (and after step 3 the single UI/API port) on host loopback for the host agent and browser, the workload can still reach them. The RFD fix as written is implemented and is a real improvement (it closes everything unpublished and removes name resolution) but does not satisfy "`:50100` unreachable from a workload". Not papered over: the acceptance box is left unticked.

### Open issues

- Published-port reachability from workloads (above). Candidate fixes for the human: (a) accept it and rely on the app-level controls (Host/Origin guards do not stop a deliberate in-container attacker); (b) give the local gRPC listener a credential even on the local path, e.g. step 4's mTLS applied to the single-node agent (contradicts the "local listener: None" row of the RFD); (c) host firewall rule outside Docker (needs root, platform-specific); (d) stop publishing gRPC through Docker and use a unix socket bind mount (works on Linux, unreliable on Docker Desktop). Not verified on a Linux Docker Engine: only Docker Desktop 29.5.3 (arm64) was available, where `host.docker.internal` additionally reaches loopback-published ports.
- Baseline defect: `npm run build` in the UI fails on two MUI 9 type errors, so `make docker-build`/`docker build -f edge/docker/Dockerfile` cannot produce an image from a clean checkout. Not fixed in step 1 (step 3 must produce a UI build, see there).
- The single-node install path was exercised only as the extracted docker block of install.sh, not the whole script (binaries/launchd parts need the real machine).
- Linux/amd64 and a real worker host were not tested.

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
