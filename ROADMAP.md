# Roadmap to MVP

This is a living, point-in-time planning doc — unlike [RFDs](RFDs/), which capture durable design decisions, this file tracks *what's done, what's not, and what order to build it in*. Expect it to go stale and get rewritten; that's fine, that's what it's for. Last assessed: 2026-09-07.

The root README and sub-project READMEs are now the source of truth for interfaces/ports/state; this doc should only ever add *sequencing and prioritization* on top of what they say, never contradict them. If you find a contradiction, the READMEs win — fix this doc, not the other way around.

## What "MVP" means here

Per the [root README](README.md#what-stays), the flagship experience is: point Underleaf at a git repo containing an app manifest, deploy it onto a cluster, see it running, and optionally expose it to the internet through the Cloud Gateway with usage tracked for billing. That's the bar for MVP — not full production-hardening of every subsystem.

## Current state (verified against code, not just docs)

The root README and every sub-project README have been corrected to match this — README content and this table should no longer disagree. If they ever do again, trust the code, re-verify, and fix whichever doc is wrong (usually the one that was written first and drifted).

| Area | Status | Detail |
| ---- | ------ | ------ |
| Tunnels (yamux) | **Substantially built** | Real yamux sessions on both ends: [edge/agent/lib/conn_client/utils.go](edge/agent/lib/conn_client/utils.go) (agent side, with reconnect/backoff) and [cloud/conn_worker/service/connection_manager.go](cloud/conn_worker/service/connection_manager.go) (conn_worker side). |
| Cluster-cloud mTLS | **Partially built** | [shared/clients/cloud_client.go](shared/clients/cloud_client.go) already has a CSR/`RequestCertificate` flow against a `/registration/certificate` endpoint. Working dev certs exist locally in `certs/`. Server-side enforcement (nginx client-cert validation, cert renewal) is unverified/incomplete. |
| App manifest (repo-backed `deploy gh:<user>/<repo>`) | **Not started** | A data model exists ([shared/types/app.go](shared/types/app.go): `AppSpec`/`App`/`Container`), but there's no manifest file format, no git-fetch, no `deploy` command in `orcli` or `ufagent`, and no `ufctl` binary anywhere in the code — only in prose (now marked as such in the [root README](README.md#what-stays)). |
| Local core loop (Orchestrator + Agent + containers + node/stream mgmt) | **Substantially built** | Per git history: registration, container CRUD over REST+gRPC, node/stream management. |
| Cloud API (billing, clusters, subscriptions) | **Substantially built** | Per git history. Actual interfaces: `:8080` internally (`/api/v2/cloud/`), fronted by nginx on `:443`. |
| Cloud infra (docker-compose, nginx, Dockerfiles) | **Freshly added, unverified end-to-end** | Introduced in the most recent WIP commit; see track 1. |
| Conn Worker REST API | **Confirmed not implemented** | [service.go](cloud/conn_worker/service/service.go) only serves `/health` — none of the tunnel CRUD routes sketched in [cloud/conn_worker/README.md](cloud/conn_worker/README.md) exist yet. The gRPC side (yamux tunnel mgmt) is real and implemented, under `CreateConnection`/`NewStream`/etc. |
| UIs | **Uneven** | `orchestrator_ui` and `account_ui` have a real `api`/`datastore`/`components`/`pages` layer per [RFDs/UI-STANDARD.md](RFDs/UI-STANDARD.md). `cockpit_ui` is **still the unmodified `create-vite` scaffold** — no API integration, no TanStack Query dependency, nothing built. |
| E2E tests | **Thin** | Framework exists ([e2e_testing/](e2e_testing/README.md)), but coverage is currently just health-check tests, no golden-path test. |
| Edge release CI | **Broken** | [.github/workflows/edge-publish-binaries.yaml](.github/workflows/edge-publish-binaries.yaml) points at a nonexistent `edge/go.mod` (module lives at repo root now). See track 1. |

**Net effect:** the manifest/deploy feature — the one thing the product is named for — is the actual bottleneck, not the gateway/mTLS work that used to top the README's TODO list before this doc replaced it.

## Tracks

### 1. De-risk the new cloud infra
**Status:** not started · **Blocks:** nothing directly, but de-risks 3 & 4 · **Size:** small

Confirm `docker compose up` actually brings up cloud_api + conn_worker + nginx + postgres + redis and they can reach each other, using `configs/local/*.yaml`. This stack was added in the last WIP commit and hasn't been exercised end-to-end.

**Also broken today, found while verifying this doc:** [.github/workflows/edge-publish-binaries.yaml](.github/workflows/edge-publish-binaries.yaml) sets `go-version-file: edge/go.mod` and `cache-dependency-path: edge/go.sum` — but there is no `go.mod`/`go.sum` under `edge/`; the module was consolidated to the repo root at some point and the workflow wasn't updated. The release pipeline described in the README's "Tagging Policy" section will not run as-is. Not fixed here since it's a CI/pipeline change, not a doc change — flagging for a deliberate fix.

### 2. Manifest & deploy (the flagship feature)
**Status:** not started · **Blocks:** MVP demo, golden-path e2e test (track 6) · **Size:** large — the critical path

- **Design first:** pick the manifest file format and how it maps to `AppSpec`. This is a real design decision, not something to back into via code — write it up as an RFD (repurpose the currently-empty [RFDs/RFD-2.md](RFDs/RFD-2.md), or add a new one; RFD-2's title "Connection Architecture" doesn't fit this topic, so probably a new RFD).
- **Orchestrator:** git-fetch service (clone/pull at a ref), manifest parser (repo file → `AppSpec`), a reconciler that turns `AppSpec` into running `Container`s via the existing container primitives.
- **CLI:** an actual `deploy` command on `orcli` (or the `ufctl` name if that's being revived).
- **UI:** an "Apps" view in the Orchestrator UI, alongside the existing Containers view.

### 3. Finish Cloud Gateway integration
**Status:** core plumbing done, integration incomplete · **Blocks:** nothing in 2 · **Size:** medium — can run in parallel with track 2

Yamux tunneling (gRPC side) works on both ends already. Confirmed missing: the REST management API in conn_worker (currently only serves `/health` — see [cloud/conn_worker/README.md](cloud/conn_worker/README.md)), which Cloud API needs in order to create/list/inspect tunnels. Also needs wiring `orcli streams new` end-to-end through Cloud API → Conn Worker → Agent, matching the sequence diagrams already documented in [cloud/docker/nginx/README.md](cloud/docker/nginx/README.md).

### 4. Finish cluster-cloud mTLS
**Status:** client-side scaffolding done, server-side unverified · **Blocks:** nothing in 2; only blocks the *cloud-connected* path · **Size:** medium — can run in parallel with 2 & 3

Confirm/finish the cloud_api server-side cert-issuance endpoint, agent-side cert storage + renewal, and nginx actually enforcing client-cert validation per its documented design in [cloud/docker/nginx/README.md](cloud/docker/nginx/README.md). Not required for a local-only MVP demo.

### 5. Billing / Cloud API polish
**Status:** substantially built · **Blocks:** monetization, not the technical MVP · **Size:** defer

Billing accounts, subscriptions, and usage events already exist per git history. Matters for charging people, not for proving the product works — sequence after the technical MVP.

### 6. Golden-path e2e test
**Status:** blocked on track 2 · **Size:** small once track 2 lands

Add one real end-to-end test to [e2e_testing/](e2e_testing/README.md): deploy from a manifest → container running → (optionally) reachable via the gateway. Today's e2e coverage is health checks only.

## Sequencing

```
1 (infra check) ──┐
                   ├─→ 2 (manifest/deploy) ──→ 6 (golden-path e2e)
                   ├─→ 3 (gateway integration)
                   └─→ 4 (mTLS)

5 (billing polish) — after the above, not blocking
```

Tracks 2, 3, and 4 don't block each other and can run in parallel. Track 2 is the heaviest lift and the only track that's genuinely zero-progress — staff it first and heaviest, since it's what the product is named for.

## Open decisions

- Manifest file format and its RFD (see track 2).
- Whether `ufctl` is the intended final CLI name for the deploy command, or whether `orcli`/`ufagent` absorb that role permanently — no `ufctl` binary exists today (the README now says so explicitly rather than implying otherwise).
- What RFD-2 should actually cover, now that "Connection Architecture" content already lives informally in `cloud/conn_worker/README.md` and `cloud/docker/nginx/README.md`.
