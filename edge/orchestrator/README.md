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
| http | REST API (JSON only, no static assets) for the Orchestrator UI and integrations | `http://127.0.0.1:9090/api/v1/` |

Routes today: nodes, containers, logs, deployments, `/health` (see `interface/rest/*.go`). CORS and the Origin guard allow `http://localhost:5173` and `http://localhost:5183`.

## Security model

The orchestrator has **no per-user authentication** on the REST API, the Agent gRPC, or the CLI socket. Anyone who can reach a listener can deploy workloads (which run on the agent's Docker daemon), read container logs, and open metered streams. The trust boundary is therefore the local host, and the controls below keep it there.

| Control | Behavior |
| ------- | -------- |
| Loopback bind | All TCP listeners (REST `:9090`, agent gRPC `:50100`/`:50101`, agent REST) bind `127.0.0.1`. `UNDERLEAF_BIND_ADDR` overrides it. |
| Fail-closed remote bind | A non-loopback bind panics at startup unless the listener is protected by `UNDERLEAF_CLUSTER_TOKEN` (gRPC only) or you set `UNDERLEAF_INSECURE_ALLOW_REMOTE=1`. The REST API cannot be token-protected today, so it always needs the explicit opt-in. |
| Cluster token | If `UNDERLEAF_CLUSTER_TOKEN` is set on both sides, every agent↔orchestrator gRPC call must carry it (constant-time check, unary and streaming). The link is still plaintext, so the token gates access but doesn't stop a network sniffer. Use it with a trusted network until mTLS lands. |
| CLI sockets | `/tmp/undf-orch.sock` and `/tmp/undf-agent.sock` are created `0600`. The CLI refuses to dial a socket not owned by the current user, since requests can carry GitHub tokens. |
| Browser guards (REST) | Requests need a loopback `Host` (DNS rebinding), an allowlisted `Origin` if one is sent (CSRF), and `Content-Type: application/json` on POST/PUT/PATCH. The log WebSocket uses the same Origin allowlist. Extend with `UNDERLEAF_ALLOWED_ORIGINS` / `UNDERLEAF_ALLOWED_HOSTS` (comma-separated). |
| SQL | The `SqlQuery` RPC and `orcli sql` exist only in `-tags devmode` builds, and run on a read-only connection with `ATTACH` disabled. |
| Docker | The image binds all interfaces inside the container (required for port publishing) and sets the insecure opt-in; `install.sh` publishes the ports on `127.0.0.1` only. Keep it that way, or set a cluster token and drop the opt-in. |
| Network isolation | `install.sh` runs the Orchestrator container on its own user-defined Docker network (`underleaf-control`); the agent puts every workload on a different one (`underleaf-workloads`, override `UNDERLEAF_WORKLOAD_NETWORK`; the default bridge, `host` and the control network are refused). Unpublished Orchestrator ports (today the REST port) and the Orchestrator's DNS name are unreachable from a workload. **Limit:** Docker still lets a container on another bridge reach a *published* port through the Orchestrator's container IP (and, on Docker Desktop, through `host.docker.internal`), so the published gRPC and UI ports remain reachable from a workload; network separation alone does not make loopback a trust boundary. See RFD 5 journal, step 1. |

Not covered yet: REST authentication (needed before exposing the UI beyond localhost), TLS between agent and orchestrator, and per-service container hardening (see `edge/agent/service/docker_service.go`).

## Running locally

From `edge/`:

```bash
make build-dev   # builds orcli, orch-server, migrate, ufagent, ufagentd into edge/bin
make run         # starts orch-server, ufagentd, and the Orchestrator UI via overmind
```

See [edge/Procfile](../Procfile) and [edge/Makefile](../Makefile) for the exact process definitions and build targets, and the [root README](../../README.md#development) for the full local dev workflow.
