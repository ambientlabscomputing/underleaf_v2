# Orchestrator

Main control-plane process for a Underleaf cluster. One Orchestrator runs per cluster (on the manager node) and coordinates Agents to realize the repo-backed app manifest model. See the [root README](../../README.md) for how this fits into the overall architecture.

| Info | Value |
| ---- | ----- |
| Language | Golang |
| Architecture | Hexagonal Domain Driven Design |
| Storage | Sqlite3 |

## Directory Structure

```
cmd/
    cli/      -- orcli binary (operator/user CLI)
    server/   -- orch-server binary (long-running daemon)
    migrate/  -- migrate binary (applies repository/migrations)
interface/
    cli/            -- orcli command implementations
    grpc_public/    -- Agent <-> Orchestrator gRPC (:50100)
    grpc_private/   -- internal/orchestrator-to-orchestrator gRPC
    rest/           -- REST API for the Orchestrator UI (http://127.0.0.1:9090/api)
repository/
    migrations/     -- Sqlite schema migrations
service/            -- domain/business logic
types/              -- domain types
utils/              -- shared helpers local to the orchestrator
```

## Interfaces

| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| gRPC | Unix socket management API for CLI | `unix:/tmp/undf-orch.sock` |
| gRPC | Agent-Orchestrator communication | `127.0.0.1:50100` |
| http | REST API at `/api/v1/` and, from the same listener and origin, the built Orchestrator UI at `/` (served from `UNDERLEAF_UI_DIR`; unset means API only) | `http://127.0.0.1:9090/` |

Routes today: nodes, containers, logs, deployments, `/health` (see `interface/rest/*.go`). There is **no CORS**: the UI is same-origin (served by the Orchestrator, or proxied by the Vite dev server), and cross-origin callers are refused by the Origin guard.

## Security model

The orchestrator has **no per-user authentication** on the REST API, the Agent gRPC, or the CLI socket. Anyone who can reach a listener can deploy workloads (which run on the agent's Docker daemon), read container logs, and open metered streams. The trust boundary is therefore the local host, and the controls below keep it there.

| Control | Behavior |
| ------- | -------- |
| Loopback bind | All TCP listeners (REST `:9090`, agent gRPC `:50100`/`:50101`, agent REST) bind `127.0.0.1`. `UNDERLEAF_BIND_ADDR` overrides it. |
| Fail-closed remote bind | A non-loopback bind panics at startup unless the listener is protected by `UNDERLEAF_CLUSTER_TOKEN` (gRPC only) or you set `UNDERLEAF_INSECURE_ALLOW_REMOTE=1`. The REST API cannot be token-protected today, so it always needs the explicit opt-in. |
| Cluster token | If `UNDERLEAF_CLUSTER_TOKEN` is set on both sides, every agent↔orchestrator gRPC call must carry it (constant-time check, unary and streaming). The link is still plaintext, so the token gates access but doesn't stop a network sniffer. Use it with a trusted network until mTLS lands. |
| CLI sockets | `/tmp/undf-orch.sock` and `/tmp/undf-agent.sock` are created `0600`. The CLI refuses to dial a socket not owned by the current user, since requests can carry GitHub tokens. |
| Browser guards (REST + UI) | Apply to every request on the listener, including UI assets and WebSocket upgrades: a loopback `Host` (DNS rebinding); an `Origin`, if one is sent, that is this server's own origin (its authority equals the request's `Host`, so the UI works on any published port) or is allowlisted (CSRF); `Content-Type: application/json` on POST/PUT/PATCH. The log WebSocket uses the same Origin policy. Extend with `UNDERLEAF_ALLOWED_ORIGINS` / `UNDERLEAF_ALLOWED_HOSTS` (comma-separated). Responses carry `X-Frame-Options: DENY` / CSP `frame-ancestors 'none'` / `nosniff`, so another site cannot frame the UI. |
| SQL | The `SqlQuery` RPC and `orcli sql` exist only in `-tags devmode` builds, and run on a read-only connection with `ATTACH` disabled. |
| Docker | The image runs only `orch-server` (no nginx): UI and API on `:9090`, agent gRPC on `:50100`. It binds all interfaces inside the container (required for port publishing) and sets the insecure opt-in; `install.sh` publishes the ports on `127.0.0.1` only. Keep it that way, or set a cluster token and drop the opt-in. |
| Stream port denylist | A gateway stream publishes a local port to the internet, so it may never target the control plane. [`shared/utils/controlports.go`](../../shared/utils/controlports.go) defines the denied set once: Orchestrator REST/gRPC (`9090`, `50100`), agent REST/gRPC (`9091`, `50101`, `50103`), the Docker daemon API (`2375`/`2376`), the **configured** REST ports from `UNDERLEAF_CONFIG`, plus anything in `UNDERLEAF_EXTRA_CONTROL_PORTS` (comma-separated; set it if you publish the control plane on other host ports). Invalid ports (`<1`, `>65535`) are refused too. Enforced twice: at stream creation in the Orchestrator (before anything is provisioned in the cloud) and in the agent, both when it accepts the request and where it dials the local port to forward. The agent check is authoritative since only it knows its own configuration. |
| Network isolation | `install.sh` runs the Orchestrator container on its own user-defined Docker network (`underleaf-control`); the agent puts every workload on a different one (`underleaf-workloads`, override `UNDERLEAF_WORKLOAD_NETWORK`; the default bridge, `host` and the control network are refused). Unpublished Orchestrator ports (today the REST port) and the Orchestrator's DNS name are unreachable from a workload. **Limit:** Docker still lets a container on another bridge reach a *published* port through the Orchestrator's container IP (and, on Docker Desktop, through `host.docker.internal`), so the published gRPC and UI ports remain reachable from a workload; network separation alone does not make loopback a trust boundary. See RFD 5 journal, step 1. |
| API tokens | `orcli tokens create --scope read\|deploy\|admin --ttl 30d [--name ...]`, `orcli tokens ls`, `orcli tokens revoke <id>`, minted **only over the owner-only unix socket** (never REST or TCP). A token is a bearer credential (`Authorization: Bearer ulat_<id>_<secret>`); only SHA-256(secret) is stored, the secret is shown once, and every token expires (1 minute to 365 days). Scopes are hierarchical (admin > deploy > read) and enforced per route from one table, `routeScopes` in [`interface/rest/auth.go`](interface/rest/auth.go): `read` = GET-style calls, `deploy` = `POST /deployments`, `admin` = everything else, and **any route not in the table requires admin** (a test fails if a route is unlisted). `deploy` runs Docker on the host and is root-equivalent. A malformed, unknown, expired or revoked token is a `401`, never a fall-through to the local path. |
| Local path vs. tokens | A request with **no** `Authorization` header is the local operator/UI and has full access (RFD 5, "Local trust"), so on a default install a token narrows nothing for a caller who simply omits it: tokens are for automation (revocable, expiring, scoped) and are what the remote listener will enforce. Set `UNDERLEAF_REQUIRE_TOKEN=1` to make every API route except `/health` require a token (the UI then needs a token and will not work without one; UI token entry is not built). |
| WebSocket tickets | Browsers cannot set headers on a WebSocket, so the UI calls `POST /api/v1/ws-tickets` (read scope) and opens the stream with `?ticket=`. A ticket is 256 random bits, valid 30 s, **single use** (consumed on first use), carries the scope of whoever minted it, works only on WebSocket routes, and is scrubbed from access logs. At most 1024 are outstanding. Revoking a token does not cancel tickets it already minted (they die within 30 s). |

| Remote listener (off by default) | Remote access (Cockpit, `ufctl connect`) is a **second** HTTP listener, enabled only by `UNDERLEAF_REMOTE_ADDR=host:port` (bind policy as for the REST listener; publish it on loopback only). It serves the same UI and API on one origin, but **every** request, static files and `/health` included, needs a valid cloud-signed assertion in the `X-Underleaf-Assertion` header; there is no unauthenticated or local-principal path. The assertion's scope (highest of `read`/`deploy`/`admin`) is checked against the **same** `routeScopes` table as API tokens; unlisted routes need admin. `Authorization` headers, API tokens, WebSocket tickets and forwarded-identity headers (`X-Subject-Id`, `X-Forwarded-*`) are not credentials there. Host and Origin guards do not apply (the browser's are the cloud's; the cloud authenticated the request before signing); JSON-only bodies still do. |
| Assertions | Ed25519 JWS (`EdDSA`, `typ` `underleaf-assertion+jwt`, `kid`), ≈60 s, claims `iss`/`sub`/`aud`/`scopes`/`iat`/`exp`/`jti`, verified by [`shared/assertion`](../../shared/assertion). Rejected: wrong or missing audience (this cluster's id is the CN of the cloud-issued cluster certificate; unregistered means everything is rejected), expired or over-long lifetime, no recognised scope, unknown `kid`, bad signature, any other algorithm (`none`, HMAC, RS256), and a `jti` already seen (in-memory cache until `exp` + leeway; a restart forgets it, leaving a replay window of at most one assertion lifetime). |
| Assertion keys | The cloud's public key set (`GET {cloud}/assertion-keys`) is fetched at start and then hourly (every minute while failing, e.g. before registration), **persisted** to `<cert_dir>/assertion_keys.json` and verified from that copy, so a cloud outage or restart does not break verification. No key ever fetched means every assertion is rejected. Keys are kept until their stated `not_after`; a fetched set replaces the cache (a key the cloud withdrew is dropped at once), but an empty or invalid answer keeps the previous set. An unknown `kid` triggers at most **one refetch per minute** (failed attempts count). The fetch uses TLS pinned to the CA chain from registration (`ca_chain.pem`, never system roots); **interim:** the cloud's nginx currently presents the CA certificate itself, which has no SAN, so only the chain is verified, not the host name (checked automatically against the host name once the cloud serves a CA-issued server certificate). Plain `http` is accepted for loopback hosts only. |
| Local listener vs. assertions | The loopback listener never reads an assertion and answers `403` to any request that carries `X-Underleaf-Assertion` or `X-Subject-Id`, so tunnelled traffic misdirected to it fails closed instead of inheriting the unauthenticated local principal. |
| Tunnel path | The cloud opens a stream **toward** the agent on the agent's existing outbound connection and writes `UNDERLEAF-REMOTE/1\n`; the agent splices only such streams to the one configured address (`UNDERLEAF_REMOTE_TARGET`), never to a destination the stream names, and refuses all cloud-opened streams when it is unset. Gateway streams run the other way (agent-opened, bound to a port the agent chose), and the remote listener's ports (`50110`, `UNDERLEAF_REMOTE_ADDR`, `UNDERLEAF_REMOTE_TARGET`) are on the stream denylist. The agent keeps a connection open for this with `UNDERLEAF_REMOTE_CONNECTION_ID`. |
| `ufctl login` / `connect` | `ufctl login` runs a device flow and stores a revocable device key at `<user config dir>/underleaf/ufctl/device.json` (dir `0700`, file `0600`, refused if loose). `ufctl connect <cluster>` serves `http://localhost:<port>` on **127.0.0.1 only** and forwards to `{cloud}/clusters/<id>/remote/...` with the device key; it checks Host (DNS rebinding), Origin and `Sec-Fetch-Site` (cross-site pages), rejects dot-dot and encoded-slash paths, and strips browser-supplied credentials. The device key is sent only to the cloud URL it was issued for, over https (or loopback http). **Cloud side not built yet:** see the RFD 5 journal, step 7b. |

Not covered yet: TLS between agent and orchestrator, UI login or token entry for strict mode, and per-service container hardening (see `edge/agent/service/docker_service.go`).

## Running locally

From `edge/`:

```bash
make build-dev   # builds orcli, orch-server, migrate, ufagent, ufagentd into edge/bin
make run         # starts orch-server, ufagentd, and the Orchestrator UI via overmind
```

See [edge/Procfile](../Procfile) and [edge/Makefile](../Makefile) for the exact process definitions and build targets, and the [root README](../../README.md#development) for the full local dev workflow.
