# Roadmap to MVP

This is a living, point-in-time planning doc — unlike [RFDs](RFDs/), which capture durable design decisions, this file tracks *what's done, what's not, and what order to build it in*. Expect it to go stale and get rewritten; that's fine, that's what it's for. Last assessed: 2026-09-08.

The root README and sub-project READMEs are now the source of truth for interfaces/ports/state; this doc should only ever add *sequencing and prioritization* on top of what they say, never contradict them. If you find a contradiction, the READMEs win — fix this doc, not the other way around.

## What "MVP" means here

Per the [root README](README.md#what-stays), the flagship experience is: point Underleaf at a git repo containing an app manifest, deploy it onto a cluster, see it running, and optionally expose it to the internet through the Cloud Gateway with usage tracked for billing. That's the bar for MVP — not full production-hardening of every subsystem.

## Current state (verified against code, not just docs)

The root README and every sub-project README have been corrected to match this — README content and this table should no longer disagree. If they ever do again, trust the code, re-verify, and fix whichever doc is wrong (usually the one that was written first and drifted).

| Area | Status | Detail |
| ---- | ------ | ------ |
| Tunnels (yamux) | **Substantially built** | Real yamux sessions on both ends: [edge/agent/lib/conn_client/utils.go](edge/agent/lib/conn_client/utils.go) (agent side, with reconnect/backoff) and [cloud/conn_worker/service/connection_manager.go](cloud/conn_worker/service/connection_manager.go) (conn_worker side). |
| Cluster-cloud mTLS | **Done end-to-end** | See track 4 (done). Full CSR → sign → enforce → renew pipeline, verified against a real live stack with a real signed certificate (not just by code inspection): agent-side CSR generation/persistence ([edge/orchestrator/service/registration_service.go](edge/orchestrator/service/registration_service.go)), cloud_api CA signing ([cloud/cloud_api/src/cloud_api/lib/cert_lib.py](cloud/cloud_api/src/cloud_api/lib/cert_lib.py)), a new `POST /registration/renew` endpoint authenticated by the caller's own still-valid mTLS cert, and nginx's `ssl_verify_client` enforcement. `docker-compose.yaml` no longer publishes `cloud_api:8080`/`conn_worker:9021` to the host — neither service does its own cert validation, both fully trust nginx having done it, so a direct host route was a full auth bypass. |
| App manifest (repo-backed `deploy gh:<user>/<repo>`) | **Done end-to-end, including UI** | See track 2 (done). `orcli deploy gh:<user>/<repo>` and the unified `ufctl deploy gh:<user>/<repo>` both work end-to-end: manifest resolution ([edge/orchestrator/service/manifest_service.go](edge/orchestrator/service/manifest_service.go)), a compile/observe/diff/plan reconciler ([reconciler_service.go](edge/orchestrator/service/reconciler_service.go)), and agent-side container/volume lifecycle + build-from-source ([docker_service.go](edge/agent/service/docker_service.go)) are all real and verified against the live `n8n` and `hello-world` repos. Reconcile runs async (`status`: `in_progress`/`succeeded`/`failed`, polled by the CLI and the Orchestrator UI's Deployments view) rather than blocking the request for the whole reconcile. A repo maps to at most one tracked deployment, so redeploying the same repo updates that record in place instead of piling up duplicate rows with an empty last-applied snapshot each time. Design in [RFDs/RFD-3.md](RFDs/RFD-3.md). |
| Local core loop (Orchestrator + Agent + containers + node/stream mgmt) | **Substantially built** | Per git history: registration, container CRUD over REST+gRPC, node/stream management. |
| Cloud API (billing, clusters, subscriptions) | **Substantially built** | Per git history. Actual interfaces: `:8080` internally (`/api/v2/cloud/`), fronted by nginx on `:443`. |
| Cloud infra (docker-compose, nginx, Dockerfiles) | **Verified end-to-end** | `docker compose up` now builds and brings up cloud_api + conn_worker + nginx + postgres + redis, with healthchecks/depends_on wired and reachability confirmed (cloud_api migrates against postgres and serves `/health` both directly and through nginx; conn_worker serves its health route and starts cleanly against redis). See track 1 (done). |
| Cloud Gateway (tunnel create/list/inspect via `orcli streams`) | **Done end-to-end** | See track 3 (done). Cloud API talks to Conn Worker over gRPC, not REST — [service.go](cloud/conn_worker/service/service.go) only ever needed to serve `/health`; the tunnel CRUD sketch that used to live in [cloud/conn_worker/README.md](cloud/conn_worker/README.md) was unused and has been removed. Fixed the gRPC↔protobuf field-mapping bug in [interface/grpc/server.go](cloud/conn_worker/interface/grpc/server.go), implemented the previously-unimplemented agent-side stream RPCs ([edge/agent/interface/grpc_public/server/server.go](edge/agent/interface/grpc_public/server/server.go)), and wired up `orcli streams ls` end-to-end. |
| UIs | **Uneven** | `orchestrator_ui` and `account_ui` have a real `api`/`datastore`/`components`/`pages` layer per [RFDs/UI-STANDARD.md](RFDs/UI-STANDARD.md). `cockpit_ui` is **still the unmodified `create-vite` scaffold** — no API integration, no TanStack Query dependency, nothing built. |
| E2E tests | **Thin** | Framework exists ([e2e_testing/](e2e_testing/README.md)), but coverage is currently just health-check tests, no golden-path test. |
| Edge release CI | **Fixed** | [.github/workflows/edge-publish-binaries.yaml](.github/workflows/edge-publish-binaries.yaml) now points `setup-go` at the root `go.mod`/`go.sum`; all four edge binaries build locally with `working-directory: edge` unchanged. See track 1 (done). |

**Net effect:** the manifest/deploy feature — the one thing the product is named for — now works fully end-to-end, backend through UI, not the gateway/mTLS work that used to top the README's TODO list before this doc replaced it.

## Tracks

### 1. De-risk the new cloud infra
**Status:** done · **Blocks:** nothing directly, de-risked 3 & 4 · **Size:** small

`docker compose up` now builds cloud_api + conn_worker from their local Dockerfiles and brings up the full stack (cloud_api, conn_worker, nginx, postgres, redis) with `depends_on`/healthchecks wired between them. Verified end-to-end: cloud_api runs its alembic migration against postgres on startup and serves `/api/v2/cloud/health` both directly and through nginx's HTTPS proxy; conn_worker serves its health route (`/api/v2/connections/health`) and starts cleanly against redis.

Along the way, fixed real config bugs that would have silently broken the stack (none were caught by the docs, only by actually running it):
- `cloud_api`'s Dockerfile wasn't running alembic migrations at all — postgres would have stayed schema-less. It now runs `alembic upgrade head` before starting the API.
- `configs/local/cloud_api.yaml` had no `db.host` override, so it defaulted to `localhost:5432` — unreachable from inside the container. Pointed it at `postgres:5432`.
- `configs/local/conn_worker.yaml`'s `gateway`/`redis` sections were partial; the Go config loader replaces those structs wholesale rather than merging field-by-field, so the missing fields (ports, timeouts, TTL) were silently zeroed. Filled in the complete sections.

**Also fixed:** [.github/workflows/edge-publish-binaries.yaml](.github/workflows/edge-publish-binaries.yaml) pointed `setup-go` at a nonexistent `edge/go.mod`/`edge/go.sum` from before the Go module was consolidated to the repo root. Now points at root `go.mod`/`go.sum`; confirmed all four edge binaries (`ufagent`, `ufagentd`, `orcli`, `orch-server`) still build locally with `working-directory: edge` unchanged.

### 2. Manifest & deploy (the flagship feature)
**Status:** done · **Blocks:** nothing (unblocks golden-path e2e test, track 6) · **Size:** large — the critical path

Design is written up in [RFDs/RFD-3.md](RFDs/RFD-3.md) (manifest format, reconcile architecture, `ufctl` gateway design). Done:
- **Manifest format:** `.underleaf/deploy.yaml`, backward-compatible with the pre-v2 format already checked into `ambientlabscomputing/n8n` (`image:`) and `ambientlabscomputing/hello-world` (`build:`). Resolved via GitHub's Contents API, no clone needed ([manifest_service.go](edge/orchestrator/service/manifest_service.go)).
- **Orchestrator:** a real compile → observe → diff → plan → execute reconciler ([reconciler_service.go](edge/orchestrator/service/reconciler_service.go)) — topologically sorted, diffs against agent-observed state plus a persisted last-applied snapshot, never auto-deletes volumes.
- **Agent:** container/volume lifecycle RPCs added to the existing `AgentPublic` gRPC service, including build-from-source (tarball download → hardened extraction → `ImageBuild`) ([docker_service.go](edge/agent/service/docker_service.go)). Two of v1's hardening defaults (cap-drop + read-only-rootfs, and a flat memory ceiling) were tried and dropped after they crash-looped real images (`nginx:alpine`, `n8n`) — see the comments in that file.
- **CLI:** `orcli deploy gh:<user>/<repo> [--ref] [--token]`, plus a new `ufctl` binary — a thin, non-duplicative gateway mounting `orcli`'s and `ufagent`'s command packages onto one root, matching what the public n8n/hello-world docs already tell users to run.
- **Async reconcile:** `POST /deployments` resolves+persists synchronously (fast) and reconciles in a background goroutine, using the same global `Status` (`in_progress`/`succeeded`/`failed`) convention already established by [shared/types/status.go](shared/types/status.go) and `RegistrationService`. `orcli deploy` polls for the outcome (mirroring `orcli cloud register`'s poll loop) instead of blocking on one long RPC; REST returns `202 Accepted`.
- **Deploy idempotency:** a source repo maps to at most one tracked `Deployment` row ([deployment_repository.go](edge/orchestrator/repository/deployment_repository.go)'s `GetDeploymentByRepo`/`UpdateSpec`). Redeploying the same repo updates that record's spec/ref in place rather than inserting a new row — needed because each deployment ID owns its own last-applied snapshot, so a fresh row every redeploy meant the reconciler always saw the existing container as "not mine" and adopted it instead of applying spec changes.
- **UI:** a "Deployments" view in the Orchestrator UI ([pages/Deployments.tsx](edge/orchestrator_ui/src/pages/Deployments.tsx)) — trigger a deploy, see a live Status column poll `in_progress` → `succeeded`/`failed` via TanStack Query (`refetchIntervalInBackground: true`, so it keeps polling even if the tab isn't focused).

Deferred by design (see RFD-3), not planned for MVP: Docker network creation (manifests' `networks:` is parsed and stored but not acted on), per-service resource limits (no flat default is safe for arbitrary images — see the docker_service.go comment above), and end-to-end port exposure through the Cloud Gateway (the manifest's `expose:` field is parsed/stored but unwired — that's track 3/4 territory).

### 3. Finish Cloud Gateway integration
**Status:** done · **Blocks:** nothing in 2 · **Size:** medium — ran in parallel with track 2

Turned out the originally-stated blocker (a missing REST management API in conn_worker) was stale: Cloud API already talks to Conn Worker directly over gRPC (`cloud_api/lib/conn_worker_client`) and already has its own REST CRUD for connections/streams; the REST sketch in [cloud/conn_worker/README.md](cloud/conn_worker/README.md) was unused by anything and has been dropped in favor of documenting the real gRPC path. The actual gaps, found by tracing the full `orcli streams new` chain, and now fixed:
- **conn_worker gRPC↔protobuf field mapping** ([interface/grpc/server.go](cloud/conn_worker/interface/grpc/server.go)) was dropping `id`/`state`/`status`/timestamps on `Connection`/`Stream` responses and the `type`/`port` fields on `NewStream` requests — Cloud API would have received connections/streams with empty IDs. Now maps every field via `connectionToProto`/`streamToProto`.
- **Agent-side stream RPCs were unimplemented.** `AgentGRPCPublicServer` ([edge/agent/interface/grpc_public/server/server.go](edge/agent/interface/grpc_public/server/server.go)) now implements `NewStream`/`CloseStream`/`GetStream`/`ListStreams`, delegating to the agent's `ConnectionService` (previously orphaned, now constructed and started in `NewService()` — [edge/agent/service/service.go](edge/agent/service/service.go)), which wraps the already-working yamux `conn_client.ConnClient`.
- **`orcli streams ls`** was a stub. The orchestrator-private `GetStream`/`ListStreams` RPCs are now implemented ([edge/orchestrator/interface/grpc_private/server.go](edge/orchestrator/interface/grpc_private/server.go)), and the CLI command lists real stream state with `--connection-id`/`--state` filters.

### 4. Finish cluster-cloud mTLS
**Status:** done · **Blocks:** nothing in 2; unblocks the *cloud-connected* path · **Size:** medium

The CSR/issuance pipeline (agent CSR generation, cloud_api CA signing, nginx `ssl_verify_client` enforcement) was already substantially implemented going into this track, but had never actually been exercised end-to-end with a real signed certificate — only reviewed by reading the code. Doing that exercise is what this track actually consisted of, and it surfaced real bugs the code review had missed:

- **Fixed three correctness bugs:** `GetTLSConfig` ([shared/clients/utils.go](shared/clients/utils.go)) panicked instead of returning errors on missing/malformed cert files; `requestCertificate` ([edge/orchestrator/service/registration_service.go](edge/orchestrator/service/registration_service.go)) silently swallowed cert-file write errors while still reporting success; cloud_api's CA cert/key path ([cloud/cloud_api/src/cloud_api/config.py](cloud/cloud_api/src/cloud_api/config.py)) resolved to two genuinely different files depending on whether it was started via Docker or bare-metal `make run`, leaving a stale duplicate CA keypair on disk (deleted).
- **Closed a real authentication bypass:** `docker-compose.yaml` published `cloud_api:8080` and `conn_worker:9021` straight to the host. Neither service validates client certs itself — `cloud_api`'s `x_subject_id_token_middleware_` mints a full access token for any request carrying an `X-Subject-Id` header with zero cryptographic check, and `conn_worker`'s `ConnectionManager` accepts a yamux tunnel for any connection whose `Host` header maps to an active connection ID in Redis, also with no cert check. Both fully depend on nginx being the *only* path in. Direct host access let anyone skip nginx's mTLS gate entirely. Both ports are now internal-only on the compose network.
- **Added renewal**, which didn't exist before: a new `POST /registration/renew` endpoint on cloud_api, authenticated by the caller's own still-valid mTLS client certificate rather than a one-time token (reaching the endpoint at all is proof of holding a valid cert, since nginx only forwards `X-Subject-Id` after successful verification) — plus an orchestrator-side periodic check (`CheckAndRenewCertificate`, 30-day threshold against the 730-day cert lifetime) wired into a background goroutine in [main.go](edge/orchestrator/cmd/server/main.go).
- **Added test coverage** where there was none: 11 new Go tests (`shared/clients`, `edge/orchestrator/service`) and 12 new Python tests (`cloud_api` had no unit-test infrastructure at all before this).
- **Found and fixed a real, previously-undetected bug** while manually driving the renewal flow against a live docker-compose stack with a real signed cert: [cloud_api/interface/deps.py](cloud/cloud_api/src/cloud_api/interface/deps.py)'s `x_subject_id_token_middleware_` injected the mTLS-derived Bearer token as a raw ASGI header tuple `(b"Authorization", ...)` — capitalized. ASGI requires header names in that list to be lowercase, and Starlette's header lookups don't renormalize case when scanning it, so the injected header was silently invisible to every downstream read, including the exact `APIKeyHeader` dependency it exists to satisfy — the middleware logged success while the request still 401'd. This meant **every mTLS-authenticated endpoint had been unreachable in practice** since it was written, masked because nothing had ever exercised it with a real cert before. One-line fix (lowercase the header name), now covered by regression tests.
- **Verified live, end-to-end, against a rebuilt stack:** real user signup → device registration → approval → CSR → real signed cert (correct CN/EKU/SAN/730-day validity, verified with `openssl verify`) → renewal using only the existing cert (no token) → new cert with a different serial but the same identity → original cert still independently usable afterward (no revocation, by design) → the freshly-renewed cert itself successfully used to renew again. Also confirmed `curl localhost:8080` / `nc localhost 9021` fail to connect post-fix, and `/registration/renew` correctly rejects no-auth, bogus-bearer, and spoofed-`X-Subject-Id` requests.

Not required for a local-only MVP demo (unregistered clusters skip mTLS entirely, per [edge/agent/service/service.go](edge/agent/service/service.go)'s gate on `node_cert.pem` existing).

### 5. Billing / Cloud API polish
**Status:** substantially built · **Blocks:** monetization, not the technical MVP · **Size:** defer

Billing accounts, subscriptions, and usage events already exist per git history. Matters for charging people, not for proving the product works — sequence after the technical MVP.

### 6. Golden-path e2e test
**Status:** unblocked, not started · **Size:** small

Track 2 is done, so nothing blocks this anymore. Add one real end-to-end test to [e2e_testing/](e2e_testing/README.md): deploy from a manifest → container running → (optionally) reachable via the gateway. Today's e2e coverage is health checks only.

## Sequencing

```
1 (infra check, done) ──┐
                         ├─→ 2 (manifest/deploy, done) ──→ 6 (golden-path e2e)
                         ├─→ 3 (gateway integration, done)
                         └─→ 4 (mTLS, done)

5 (billing polish) — after the above, not blocking
```

Tracks 2, 3, and 4 don't block each other and can run in parallel. All three are now fully done, so track 6 (golden-path e2e) is unblocked and could optionally also cover the gateway and cloud-connected mTLS paths now.

## Open decisions

- What RFD-2 should actually cover, now that "Connection Architecture" content already lives informally in `cloud/conn_worker/README.md` and `cloud/docker/nginx/README.md`.
- The mTLS CA signing key and the OAuth JWT signing key are the exact same RSA keypair (self-bootstrapped by `cloud_api/lib/auth_lib.py`, reused by `cert_lib.py` for client-cert signing and by nginx for TLS termination — see track 4). Accepted as a local-dev-only shortcut for track 4's scope; compromising one trust domain compromises both, so this needs a dedicated track (splitting the keys) before any non-local deployment.

Resolved: manifest file format and CLI naming — see [RFDs/RFD-3.md](RFDs/RFD-3.md) and track 2. `ufctl` is a real binary (a thin gateway over `orcli`/`ufagent`, not a separate implementation).
