# RFD-5 Implementation Journal

Base: main @ 782f917 · Integration branch: rfd-5/integration · Last updated: 2026-11-04 by coordinator

## Status

| Step | Title | State | Owner | Branch / PR | Needs | Updated |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | Network isolation | in review | agent-A | rfd-5/track-a | none | 2026-11-04 |
| 2 | Stream port denylist | in review | agent-A | rfd-5/track-a | none | 2026-11-04 |
| 3 | One-origin UI + API | in review | agent-A | rfd-5/track-a | none | 2026-11-04 |
| 4 | Cluster CA, listener, `nodes add`, join | not started | none | none | none | none |
| 5 | Agent link | not started | none | none | 4 | none |
| 6 | Scoped API tokens | in review | agent-A | rfd-5/track-a | 3 | 2026-11-04 |
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
- [x] Creating a stream to a control-plane port is rejected at creation (test)
- [x] Same port is rejected again at forward time in the agent (test)
- [x] Common checks green; README Security model updated

### Log

#### 2026-11-04 · agent-A · control-plane port denylist at creation and at forward time
- Did: [shared/utils/controlports.go](../shared/utils/controlports.go) (new, the single definition): `ControlPlanePorts()` / `CheckStreamPort(port)`. Denied: Orchestrator REST `9090` and gRPC `50100`, agent REST `9091` and gRPC `50101`, the odd named constants `50101`/`50103` from `config.go`, Docker API `2375`/`2376`, the **configured** REST ports read from `GetConfig(Orchestrator|Agent).Http.Port` (so `UNDERLEAF_CONFIG` overrides are honoured), plus `UNDERLEAF_EXTRA_CONTROL_PORTS` (new, for non-default published ports), and any port outside 1..65535. Enforcement points: (1) Orchestrator [`ConnectionService.NewStream`](../edge/orchestrator/service/connection_service.go) rejects with gRPC `InvalidArgument` *before* creating the cloud connection/stream; (2) agent [`ConnectionService.NewStream`](../edge/agent/service/connection_service.go) fails fast with `PermissionDenied`; (3) agent [`ConnClient.NewGatewayStream`](../edge/agent/lib/conn_client/conn_client.go), the only place a local port is dialed for forwarding, checks first, before any lock/dial. Also: when the agent refuses after the cloud stream was already created (e.g. the Orchestrator's config differs), the Orchestrator now closes the cloud-side stream and terminates the connection best-effort instead of leaving a public stream pointing at nothing. Also fixed a CLI bug: `orcli streams new --port` was declared a string flag but read with `GetInt`, so the port was always 0 ([new.go](../edge/orchestrator/interface/cli/commands/streams/new.go): `IntP`). README Security model: new "Stream port denylist" row.
- Verified:
  - `go test ./shared/utils ./edge/orchestrator/service ./edge/agent/... -count=1` -> all ok. Tests: defaults denied and wrap `ErrControlPlanePort`; ordinary ports (22, 80, 443, 3000, 5432, 8080, 5678, 65535) allowed; invalid ports (0, -1, 65536, 1<<20) refused; a `UNDERLEAF_CONFIG` yaml with `http.port` set to a non-default value is denied while its neighbour stays allowed; extra-ports env (with junk entries ignored). Orchestrator creation test uses nil cloud/agent clients, so any code path past the check would panic: ports 9090/9091/50100/50101/50103/2375/15090(extra)/0/-1/70000 all return `InvalidArgument`, no stream. Agent forward tests use a real local TCP listener on a denied port (via the extra list, and via config) and assert **zero connections ever reach it**; a negative control shows an ordinary port passes the denylist (and fails later only for lack of a cloud connection).
  - Mutation check: neutralising the check at each of the three sites in turn makes the corresponding tests fail (forward: `TestForwardRefusesControlPlanePorts`, `TestForwardNeverDialsDeniedPort`; orchestrator and agent service: nil-pointer panic from reaching the clients). Files restored; `git diff` shows only the intended change.
  - `go build ./...`, `go build -tags devmode ./...`, `go vet ./...`, `go test ./...` pass; `gofmt -l edge shared cloud` empty.
- Next: step 3.
- Blocked: none

### Decisions and deviations

- **One definition, two enforcement processes.** The Orchestrator only knows its own config; the denylist therefore unions both configs' REST ports in every process (over-denying one extra port is harmless) and offers `UNDERLEAF_EXTRA_CONTROL_PORTS` for the case it cannot know (non-default host ports where the Orchestrator container is published, e.g. `install.sh` with custom ports). The agent check is the authoritative one.
- **Docker API ports (2375/2376) are in the set** though the RFD lists only Orchestrator/agent ports: they are equally root-equivalent. Cheap to remove if undesired.
- **Real constants vs. config.go**: `config.go` names `OrchestratorGRPCPort=50101` and `AgentGRPCPort=50103`, but the listeners actually bind 50100 and 50101 (hardcoded in the two `server.go` files). Left alone (those files are Track B's); both the real values and the named ones are denied, and new constants `OrchestratorPublicGRPCPort`/`AgentPublicGRPCPort` document the real ones.
- The cloud-side stream request is not validated for the port (cloud is Track C's area).

### Open issues

- Not run end to end: creating a real stream needs the cloud stack (`cloud_api`, `conn_worker`) and a registered cluster; verified with unit tests that drive the real check functions and a real TCP listener, not through `orcli streams new` against a live cloud.
- A Docker-published port that maps the control plane to an arbitrary host port can only be denied if listed in `UNDERLEAF_EXTRA_CONTROL_PORTS`; nothing detects it automatically.

## Step 3: One-origin UI + API

**Goal:** see the RFD [Migration](RFD-5.md#migration) and the relevant section for this step.

**Acceptance:**
- [x] UI loads from the Orchestrator's single origin with no CORS configuration (evidence)
- [x] Log WebSockets work over that origin (evidence)
- [x] Dev-server workflow still works, or its replacement is documented
- [x] Common checks green; install.sh / Dockerfile / README updated

### Log

#### 2026-11-04 · agent-A · UI and API from one listener
- Did: Orchestrator REST server now serves the built UI from the API's own listener ([server.go](../edge/orchestrator/interface/rest/server.go) `newRouter`, [ui.go](../edge/orchestrator/interface/rest/ui.go)): `UNDERLEAF_UI_DIR` (set by the Docker image) points at the build; any non-API GET/HEAD gets the file or the SPA `index.html`; unknown `/api/v1/*` paths get a JSON 404 not the SPA; dotfiles never served; lookups confined by `http.Dir`; `index.html` is `no-cache`, `assets/*` `immutable`. **CORS removed** (`gin-contrib/cors` dropped from the router, `go.mod`/`go.sum` tidied: only that dependency left). Guards **not weakened**: Host/Origin/Content-Type run first on every request including assets and WebSocket upgrades. The Origin policy gained one rule, `sameOrigin` ([security.go](../edge/orchestrator/interface/rest/security.go)): an Origin is accepted if its authority equals the request's `Host` *and* that Host is itself loopback-allowed, so the UI works on whatever port it is published on with no configuration; it is also what the WebSocket upgrader now uses (`originPermitted`). Added `X-Frame-Options: DENY`, CSP `frame-ancestors 'none'`, `nosniff` (a no-login UI that can deploy workloads must not be frameable). A `logSource` seam lets tests run the real WebSocket route without an agent.
  Dockerfile: **nginx removed** ([Dockerfile](../edge/docker/Dockerfile) now `alpine:3.20` + `ca-certificates` running only `orch-server`; `docker/nginx.conf` and `docker/run-all.sh` deleted; image 93 MB -> 41 MB). Container serves UI + API on `:9090`, gRPC `:50100`. `install.sh` and `make docker-run` publish `127.0.0.1:${HTTP_PORT}:9090` (was `:80`). UI: API base is relative (`/api/v1`), the log WebSocket URL is derived from `window.location` (this also fixes it pointing at `/api/v2`, which the server never served); `vite.config.ts` proxies `/api` with `ws: true` to `ORCHESTRATOR_URL` (default `http://127.0.0.1:9090`). Fixed the two baseline TypeScript errors that made `npm run build` (hence the image build) fail ([PageShell.tsx](../edge/orchestrator_ui/src/components/PageShell.tsx), [theme.ts](../edge/orchestrator_ui/src/components/theme/theme.ts), MUI 9 API changes). READMEs updated (root, orchestrator Security model, UI).
- Verified:
  - `go test ./edge/orchestrator/interface/rest -count=1 -v`: 12 tests pass, new ones: `TestUIAndAPIShareOneOrigin` (UI, SPA fallback, asset caching, API and API-404 on one host:port), `TestNoCORSHeadersAreEverSent` (no `Access-Control-*` for any origin; foreign preflight -> 403), `TestUIAssetsAreBehindTheSameGuards` (rebound Host -> 403 on `/`, assets and API; foreign Origin -> 403 on UI; form POST -> 415 even same-origin; same-origin JSON POST reaches the handler (400); other-port origin -> 403; POST to a UI path -> 405), `TestUIDoesNotServeFilesOutsideItsRoot` (`/.env`, `/../secret.txt`, encoded variants never leak; security headers present), `TestLogWebSocketWorksOnTheUIOrigin` (real gorilla client: upgrade with same-origin, no-Origin and dev-origin; receives both lines), `TestLogWebSocketStillRejectsForeignOriginsAndHosts` (foreign origin, other port, rebound Host x3 -> HTTP 403), `TestSameOrigin` (13 cases incl. ipv6, opaque `null`, path, scheme). Pre-existing guard tests untouched and passing. Mutation check: making `originPermitted` ignore `sameOrigin` fails `TestUIAssetsAreBehindTheSameGuards` and `TestLogWebSocketWorksOnTheUIOrigin`.
  - Real image, real browser: `docker build -f docker/Dockerfile` (context = scratch copy, `bin/orch-server-linux-arm64` cross-built) succeeds with the real `npm run build` (tsc + vite). Started via the install.sh docker block (`rfd5-a-*` names, host `127.0.0.1:15291`). `curl`: `/` 200 `text/html`, `Cache-Control: no-cache`, `X-Frame-Options: DENY`, CSP `frame-ancestors 'none'`; `/api/v1/health` 200 `{"status":"OK"}`; `/containers` (SPA route) 200 html; 0 `Access-Control-*` headers; foreign `Origin` -> 403, `Host: evil.example` -> 403; `ps` shows no nginx, `netstat`: only `:9090` and `:50100`. In the browser pane at `http://127.0.0.1:15291/`: the UI rendered ("No nodes registered yet", i.e. the nodes call succeeded), network log shows only same-origin requests (`/`, `/assets/index-*.js`, `/assets/index-*.css`, `/api/v1/nodes`, `/api/v1/health`, all 200), no console errors, and `new WebSocket('ws://127.0.0.1:15291/api/v1/containers/abc/logs/stream')` from the page upgrades and delivers a message (the relay's JSON error, since no agent was running).
  - Dev-server workflow: `ORCHESTRATOR_URL=http://127.0.0.1:15291 npx vite` and the browser at `http://localhost:5183/`: page renders; `fetch('/api/v1/nodes')` 200, `POST /api/v1/deployments {}` 400 (reached the handler through the guards), and a WebSocket through the Vite proxy opens and delivers a message. So `make ui-run`/`make run` keep working with no CORS and no `VITE_API_URL`.
  - After the move, a stand-in workload on another network can reach `http://<orchestrator-ip>:9090/api/v1/health` (it is now a published port); see Open issues. `wget https://github.com` from the container works (CA certs present).
  - `go build ./...`, `go build -tags devmode ./...`, `go vet ./...`, `go test ./...` pass; `gofmt -l` empty; `npx tsc -b` 0 errors.
- Next: step 6.
- Blocked: none

### Decisions and deviations

- **nginx removed, not reduced.** One listener is the point (a tunnel exposes one port); keeping nginx as a reverse proxy would add a second process and a second set of headers for nothing. The final image stage is plain alpine.
- **UI directory by env, not `go:embed`.** Embedding needs the UI built before `go build`, which would break `go build ./...` on a clean checkout and the CI binary matrix (which builds `orch-server` without Node). A runtime directory (`UNDERLEAF_UI_DIR`) keeps the Go build independent; the host `orch-server` from `make run` serves API only and the UI comes from Vite.
- **No CORS at all** rather than a tighter CORS: with a proxy in dev and same-origin in prod there is no legitimate cross-origin caller. `VITE_API_URL` (direct-to-API dev) now requires `UNDERLEAF_ALLOWED_ORIGINS`.
- **Same-origin rule instead of listing the published port.** `install.sh` allows any `HTTP_PORT`, so a static Origin allowlist cannot cover the UI's own origin; comparing Origin to the already-validated Host is exact and cannot admit a foreign page. This is the only change to the guards, and it only adds the server's own origin.
- **Upgrade path for existing installs**: an old container keeps its old image (and `:80`-published UI that could never reach the API); `install.sh` prints how to switch. Not automated because the orchestrator's state (db, certs) lives in the container filesystem and recreating loses it.
- **UI errors fixed outside "step scope"**: the two TypeScript errors were baseline blockers for building the image; minimal MUI 9 migrations, no behaviour change. `eslint` still reports 8 pre-existing `no-restricted-imports` errors in `pages/` (untouched).

### Open issues

- **Published port = reachable by workloads (compounds step 1's open issue).** Before step 3 the REST port (`:9090` in the container) was *not* published, so a workload could not reach it; now UI and API share the published port, so a workload on another network can reach the REST API through the Orchestrator's container IP (verified above), subject only to the Host/Origin/Content-Type guards, which an attacker controls. Same underlying cause and same candidate fixes as step 1; it needs a human decision before the single-node Docker install can be called isolated. Token scopes (step 6) do not help on the no-login UI path.
- The Docker install still keeps the orchestrator's state (db, certs) in the container filesystem (no volume), and `orcli` on the host cannot reach the container's unix socket; both pre-existing, untouched.
- Not verified: the full `install.sh` (only its docker block), Linux, amd64 image, the `ufctl connect` use of this origin (step 7b).

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
- [x] Tokens created, listed, revoked over the socket (test)
- [x] Each scope (read|deploy|admin) enforced by a test, including negative cases
- [x] Database holds no plaintext token (test/inspection)
- [x] WebSocket tickets are single-use (test)
- [x] Common checks green; README Security model updated

### Log

#### 2026-11-04 · agent-A · scoped API tokens, route scopes, WebSocket tickets
- Did:
  - **Storage**: migration [`0008_api_tokens`](../edge/orchestrator/repository/migrations/0008_api_tokens.go) (`api_tokens`: id, name, scope with a CHECK constraint, `token_hash`, created/expires/revoked/last_used) and [`api_token_repository.go`](../edge/orchestrator/repository/api_token_repository.go) (exposed as `Repository.APITokens()`, a method, so `repository.go` is untouched).
  - **Service** [`token_service.go`](../edge/orchestrator/service/token_service.go): token = `ulat_<12 hex id>_<64 hex secret>` (256 random bits); only SHA-256(secret) stored; secret returned once; constant-time hash compare; every failure wraps `ErrInvalidToken` (reason internal, the HTTP layer says only "401"); mandatory expiry 1 min..365 d; hierarchical scopes `admin > deploy > read`; `last_used_at` updated at most once a minute. `AppService.Tokens()` is a method on the existing struct (no field added).
  - **Mint over the socket**: new gRPC service `OrchestratorTokens` in its own proto ([orchestrator_tokens.proto](../edge/orchestrator/interface/grpc_private/orchestrator_tokens.proto), generated files are new, the existing generated files are byte-identical after `go generate`), implemented in [tokens.go](../edge/orchestrator/interface/grpc_private/tokens.go), registered on the private unix-socket server with one line in `server.go`. CLI: [`orcli tokens create|ls|revoke`](../edge/orchestrator/interface/cli/commands/tokens/tokens.go) (`--scope read|deploy|admin`, `--ttl 30d`, units s/m/h/d/w, `--name`), mounted via `AddCommands` so `ufctl` gets it too. There is deliberately no REST route to create, list or revoke tokens.
  - **Enforcement** [auth.go](../edge/orchestrator/interface/rest/auth.go), after the unchanged guards: the whole route -> scope map is the `routeScopes` table (`/health` public; every `GET` and `POST /ws-tickets` = read; `POST /deployments` = deploy; **unlisted routes = admin**, and a test fails if any registered route is unlisted or any entry is stale). Precedence on a request: WebSocket `?ticket=` (WS routes only) > `Authorization: Bearer` > local principal. A bad ticket or bad token is always 401, never a fall-through. `403 {"error":"insufficient scope","required":"<scope>"}` on scope failure.
  - **WebSocket tickets**: `POST /api/v1/ws-tickets` (read) -> `{"ticket":"ulwt_...","expires_in":30}`; in-memory, 256 random bits, 30 s, single use (consumed on first redeem, under a mutex), carries the minter's scope, cap 1024 outstanding, and `ticket=` is redacted from the access log. UI: [client.ts](../edge/orchestrator_ui/src/api/client.ts) no longer sends the hard-coded `Authorization: Bearer TOKEN` (which would now be a 401), and [LogsService.ts](../edge/orchestrator_ui/src/api/services/LogsService.ts) / [datastore/logs.ts](../edge/orchestrator_ui/src/datastore/logs.ts) fetch a ticket before opening the socket (with a generation counter so a late ticket does not leak a socket).
  - README Security model: rows for API tokens, local path vs tokens, WebSocket tickets. RFD: caveat paragraph added under "Local trust" (step 1 finding).
- Verified (all in this worktree; `go test ./... -count=1` passes in every package):
  - [`token_service_test.go`](../edge/orchestrator/service/token_service_test.go), real migrated sqlite: create/authenticate for each scope; **no plaintext**: the stored hash equals sha256(secret) and differs from it, no column of any row contains the secret or full token, and neither `orch.db`, `-wal` nor `-shm` contains the secret bytes; 16 rejection cases (empty, prefix only, no/wrong prefix, `Bearer ` inside, wrong secret, unknown id, another id's secret, truncated, extra char, uppercase hex, the stored hash used as the secret, whitespace, newline, SQL-ish); expiry (1 s before ok, exactly at expiry rejected, long after rejected, status `expired`); revoke (revoked rejected, sibling token unaffected, idempotent keeping the first time, unknown id -> `ErrAPITokenNotFound`); create validation (unknown/empty scope, ttl 0/negative/30 s/too long, long name, control chars, max ttl allowed); `Scope.Allows` matrix incl. unknown scopes; last-used throttling.
  - [`auth_test.go`](../edge/orchestrator/interface/rest/auth_test.go), the real router with a real token service: `TestScopesEnforcedPerRoute` (3 scopes x read route / deploy route / ticket route: each allowed or `403` naming the required scope; read cannot deploy, deploy can), `TestEveryRegisteredRouteHasAnExplicitScope`, `TestUnmappedRouteRequiresAdmin` (read 403, deploy 403, admin 200), `TestBadTokensAreRejectedNotDemotedToLocal` (wrong secret, revoked, garbage, empty bearer, Basic scheme, no scheme, two tokens: all 401 + `WWW-Authenticate`; valid then expired after the clock passes; lowercase `bearer` accepted), `TestRevokedTokenStopsWorkingImmediately`, `TestLocalPathUnchangedByDefaultAndStrictModeClosesIt` (no header works by default; with `UNDERLEAF_REQUIRE_TOKEN=1` 401 everywhere except `/health`), `TestGuardsRunBeforeAuthentication` (a valid admin token does not bypass Host/Origin/Content-Type: 403/403/415), tickets: `TestWebSocketTicketIsSingleUse` (first use upgrades and streams, replay -> 401), `...Rejections` (garbage, a bearer token used as a ticket), `...Expires` (injected clock: TTL-1s ok, TTL+1s 401), `...OnlyWorkOnWebSocketRoutes` (a ticket on a REST route is not a credential and is not burned by the attempt), `TestStrictModeTicketsNeedAToken`, `TestTicketStoreUnit` (unique, scope carried, single redeem, **50 concurrent redeemers -> exactly 1 winner**, cap of 1024 and expiry frees room), `TestTicketsNeverAppearInAccessLogs`.
  - [`tokens_test.go`](../edge/orchestrator/interface/grpc_private/tokens_test.go): a real gRPC server on a real `ListenUnixPrivate` socket (mode checked `0600`, owner checked): create -> token authenticates -> list shows it, never the secret (proto bytes searched), last use recorded -> revoke -> no longer authenticates, list shows `revoked`; negative: unknown/empty scope, ttl missing/negative/too short/too long, a ttl that **wraps mod 2^64 to exactly one hour** (`3600 + 2^55` s), long name, revoke unknown (`NotFound`) and revoke without id; nothing is created by failed requests. This test found a real bug in my first version (`time.Duration(secs)*time.Second` overflow let the wrapping value through); fixed by bounding the seconds first, and confirmed the test fails without the bound.
  - Mutation checks: disabling the scope check, not consuming tickets, ignoring ticket expiry, letting a bad token fall through to local, and ignoring strict mode each fail 1-3 named tests; restored.
  - Real image + real client: image built with the real Dockerfile, run via the install.sh block (`rfd5-a-*`, host `127.0.0.1:15391`), `orcli` cross-built and run inside the container over its unix socket. `orcli tokens create --scope read|deploy|admin` ok; `--scope root` -> InvalidArgument, `--ttl 400d` -> "ttl must be between 1m0s and 8760h0m0s", `--ttl banana` -> CLI error. From the host: no header -> 200; read `GET /containers` 200; read `POST /deployments` -> **403** `{"error":"insufficient scope","required":"deploy"}`; deploy and admin `POST /deployments` -> 400 (authorized, handler rejected the bogus source); wrong secret / garbage -> 401; after `orcli tokens revoke <id>` the read token -> 401 and `ls` shows `revoked`; revoking an unknown id -> NotFound. Database copied out of the container: `sqlite3` shows only ids, scopes and 16-char hash prefixes, `grep -c <admin secret>` on the db file = 0, and exactly 1 row has `token_hash = sha256(admin secret)`. Strict container (`UNDERLEAF_REQUIRE_TOKEN=1`): no token `GET /containers` 401, `/health` 200, UI shell 200, `POST /ws-tickets` 401; read token `GET /containers` 200 and `POST /ws-tickets` 200. Browser: the UI's own log page issued `POST /api/v1/ws-tickets` -> 200 before opening its WebSocket; by hand in the page: fresh ticket upgrades once, the same ticket again -> closed 1006, `ulwt_garbage` -> closed 1006; the built JS contains no `"TOKEN"` literal.
  - `go build ./...`, `go build -tags devmode ./...`, `go vet ./...`, `go test ./...` pass; `gofmt -l edge shared cloud` empty; `npx tsc -b` 0 errors; `go mod tidy` is a no-op.
- Next: final report. Coordinator to merge.
- Blocked: none

### Decisions and deviations

- **How the unauthenticated loopback path and tokens coexist (the decision the brief asked to record).** Order: ticket (WS routes only), then `Authorization: Bearer`, then the *local principal*. No credential = the local operator/UI = admin-equivalent, exactly as RFD "Local trust" and "Principals" say (the UI has no login). A credential that is present but bad is 401 and never degrades to local. Consequence, stated plainly: **on a default install a scoped token restricts nothing for a caller who just omits the header**; scopes bite where anonymous access is not available: strict mode (`UNDERLEAF_REQUIRE_TOKEN=1`, an addition of mine, off by default so the single-node UX is unchanged) and the remote listener (7b). Strict mode is also the only mitigation available today for the published-port exposure (steps 1 and 3), at the price that the UI cannot be used (no token entry in the UI yet).
- **Scopes are hierarchical**: `deploy` includes `read`, `admin` includes both (a deploy token needs to poll `GET /deployments/:id`). `read` = every GET plus minting a WS ticket; `deploy` = `POST /deployments`; `admin` = everything else, **including any route nobody has classified** (fail closed) and, in the future, nodes and anything token-related. The RFD says admin includes "token management & nodes": there are no such REST routes today (tokens are managed over the socket only, per the RFD; the `nodes` CLI is Track B), so `admin` currently differs from `deploy` only for unlisted routes.
- **Mandatory expiry (1 min..365 d)**, no "never expires" token. RFD example uses 30d; default is 30d. A year is my cap, easy to raise.
- **Token format `ulat_<id>_<secret>`** with a public id so tokens can be listed/revoked without exposing anything, and hex so the `_` separator is unambiguous. SHA-256 (not bcrypt/argon2): the secret is 256 random bits, nothing to brute-force, and verification is on every request.
- **Tickets are not re-checked against revocation** at redemption: a revoked token's already-minted tickets live at most 30 s and only open log streams (read scope).
- **Separate proto/gRPC service for tokens** instead of new RPCs on `OrchestratorPrivate`, to keep the generated files Track B will also regenerate out of the merge. Same socket, same trust.
- **Migration id `0008_api_tokens`** (var `migrationAPITokens`). Track B's migrations will also want an `0008_*`/`0009_*`; ids are strings applied in file-name order and the table sets are disjoint, so both can coexist, but see Handoffs for the ordering caveat.
- **UI**: removed the hard-coded `Bearer TOKEN` header (it would have turned every UI call into a 401 the moment a request carries an invalid token) and moved log streaming to tickets. No token-entry UI.

### Open issues

- Tokens do not restrict a caller who omits the header on a default install (above). If the human wants tokens to matter locally, the options are strict mode by default (breaks the UI until it can hold a token) or a UI login; both reopen RFD decision 1, so not done.
- Not verified: the `ufctl` build of the `tokens` command (it is mounted through the shared `AddCommands`, and `go build ./...` including `ufctl` passes, but `ufctl tokens ...` was not run); Linux; the token path through a real tunnel (step 7b); behaviour under many concurrent requests (the last-used write is a single `UPDATE`, throttled to once a minute per token, but there is no load test); amd64.
- Observed, pre-existing, not fixed: the UI's container-logs page renders the relay's `{"error": ...}` frame (sent when the agent is unreachable) as a log line and crashes with `RangeError: Invalid time value`; seen here only because the test Orchestrator had no agent. `eslint` reports 8 `no-restricted-imports` errors in `src/pages/`.
- The orchestrator's database and certs live in the container filesystem (no volume): recreating the container loses tokens along with everything else. Pre-existing, now also affects tokens.

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

- **(agent-A, step 1) "Separate user-defined networks are isolated from each other" does not hold for published ports** (Docker 29.5.3 Desktop/arm64). Unpublished ports and DNS are isolated; `:50100` and the UI/API port, which must be published on host loopback for the host agent and browser, stay reachable from a workload via the Orchestrator's container IP, and on Docker Desktop via `host.docker.internal`. Source-IP checks cannot tell them apart (all arrive from the bridge gateway). The RFD fix is implemented as written; the "`:50100` unreachable from a workload" acceptance is not met. Caveat added to RFD "Local trust".
- **(agent-A, step 3) Step 3 makes the above worse for REST**: the REST port used to be unpublished (isolated); one origin means it is now published, so a workload can reach it too. Mitigation available today: `UNDERLEAF_REQUIRE_TOKEN=1` (step 6), which breaks the UI.
- **(agent-A, step 6) Tokens do not restrict anonymous local callers** (RFD "Local trust" keeps the loopback UI login-free); scopes matter in strict mode and for the remote listener. Details under step 6 decisions.
- **(agent-A, step 2) Streams may not target Docker's API ports (2375/2376)** either; the RFD lists only Orchestrator/agent ports.

### Open questions for the human

- **Published-port exposure to workloads (steps 1 and 3).** Pick one: (a) accept and document, with `UNDERLEAF_REQUIRE_TOKEN=1` for hardened installs; (b) authenticate the local gRPC/REST path even on one host (e.g. step 4's mTLS for the single-node agent; a UI login or token entry for REST), which contradicts the RFD's "local listener: none"; (c) Linux-only: run the Orchestrator with `--network host` bound to loopback, which workloads cannot reach, but this does not work on Docker Desktop for macOS; (d) a host firewall rule outside Docker (root, platform-specific); (e) unix-socket bind mount for gRPC (Linux only in practice). I could not test Linux.
- **Should strict token mode become the default**, given the above? It needs a way for the UI to hold a token first.
- **Persistent volume for the Orchestrator container** (db, certs, now tokens live in the container filesystem and are lost on recreate). Needed before "recreate to upgrade" is a safe instruction; not in the RFD.

### Handoffs

#### 2026-11-04 · coordinator · baseline and setup
- Did: created this journal and recorded the baseline; created `rfd-5/integration` from `main`.
- Verified: see Baseline.
- Next: spawn Track A (steps 1,2,3,6), Track B (steps 4,5,8), Track C (step 7a, then 7b once 3 and 6 are merged).
- Blocked: none

#### 2026-11-04 · agent-A · Track A (steps 1, 2, 3, 6) ready for merge
- Did: four steps on branch `rfd-5/track-a`, in order, one commit each (see `git log`). All four rows are `in review`. Step 1's acceptance box "from a workload, `:9090` and `:50100` unreachable" is deliberately unticked (see Deviations).
- Merge notes for the coordinator: (1) **Migration ids**: mine is `0008_api_tokens` (`migrationAPITokens`); if Track B also adds `0008_*`, ids are distinct strings applied in file-name order and the tables are disjoint, but rename one if you prefer a strict sequence. (2) Files shared with Track B that I touched minimally: `edge/orchestrator/interface/grpc_private/server.go` (one `Register...` line), `edge/orchestrator/interface/cli/cli.go` (one `AddCommand`), `shared/cli/utils/dependency_manager.go` (one field + one line), `edge/orchestrator_ui/public/tools/install.sh` (docker block only, plus `--network` and the published container port `9090`), `edge/docker/Dockerfile` (rewritten: no nginx; Track B's step 8 removes the two env stopgaps from it), `edge/orchestrator/README.md` Security model (rows added; keep both sides). `shared/utils/netsec.go` is untouched; new files `shared/utils/controlports.go`. (3) Step 5 (agent link) must keep `docker_service.go`'s `workloadHostConfig` and the denylist hooks in `conn_client.go`/`connection_service.go` when it rewrites the agent flow. (4) 7b needs the `routeScopes` table and `authMiddleware` in `edge/orchestrator/interface/rest/auth.go` (the remote listener's assertion scopes should map onto the same scopes) and the same-origin rule in `security.go`.
- Verified: see each step. Final: `go build ./...`, `go build -tags devmode ./...`, `go vet ./...`, `go test ./... -count=1` pass, gofmt clean, `npx tsc -b` clean.
- Next: coordinator merges `rfd-5/track-a` into `rfd-5/integration` and marks the steps `done` once the open decisions above are settled or accepted.
- Blocked: none
