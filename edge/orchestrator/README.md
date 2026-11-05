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

Not covered yet: TLS between agent and orchestrator, UI login or token entry for strict mode, and per-service container hardening (see `edge/agent/service/docker_service.go`).

## Running locally

From `edge/`:

```bash
make build-dev   # builds orcli, orch-server, migrate, ufagent, ufagentd into edge/bin
make run         # starts orch-server, ufagentd, and the Orchestrator UI via overmind
```

See [edge/Procfile](../Procfile) and [edge/Makefile](../Makefile) for the exact process definitions and build targets, and the [root README](../../README.md#development) for the full local dev workflow.
