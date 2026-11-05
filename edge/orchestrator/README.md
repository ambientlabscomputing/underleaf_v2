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
    grpc_public/    -- Agent -> Orchestrator gRPC (:50100)
    cluster/        -- the cluster listener (mTLS) and the join endpoint
link/               -- the agent link: hub of live links, command routing
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
| gRPC | Agent-Orchestrator communication, **local listener** (single-node install; unauthenticated, loopback only). Serves `OrchestratorPublic` and the agent link | `127.0.0.1:50100` |
| gRPC (mTLS) + HTTPS join | **Cluster listener**: worker nodes. Exists only after `orcli nodes add`; client certificates from the cluster CA are required | `0.0.0.0:50104` (advertised address is what nodes dial) |
| http | REST API (JSON only, no static assets) for the Orchestrator UI and integrations | `http://127.0.0.1:9090/api/v1/` |

Routes today: nodes, containers, logs, deployments, `/health` (see `interface/rest/*.go`). CORS and the Origin guard allow `http://localhost:5173` and `http://localhost:5183`.

## Security model

The orchestrator has **no per-user authentication** on the local listeners (REST and the local gRPC) or the CLI socket. Anyone who can reach one of them can deploy workloads (which run on an agent's Docker daemon), read container logs, and open metered streams. The trust boundary for those is therefore the local host, and the controls below keep it there. Anything that crosses the network does so on the **cluster listener**, which requires mutual TLS. There are no other switches: no shared token, no "allow remote" flag.

| Control | Behavior |
| ------- | -------- |
| Loopback bind | The unauthenticated TCP listeners (REST `:9090`, local gRPC `:50100`) bind `127.0.0.1`. `UNDERLEAF_BIND_ADDR` may name another interface, but `utils.ListenLocal` refuses any non-loopback address (startup fails with an explanation) unless the process is running in the Docker image (next row). Agents have no TCP listeners at all. |
| Container mode | The one recorded exception. Inside the Docker image the local listeners must bind the container's interface for port publishing to reach them, so the image sets `UNDERLEAF_BIND_ADDR=0.0.0.0` and `UNDERLEAF_CONTAINER=1`. A wildcard bind is accepted only when **both** the variable is `1` **and** the process really is in a container (`/.dockerenv` or `/run/.containerenv` exists), so exporting the variable on a bare host, or running the binary in some other container without it, widens nothing. It never applies to the cluster listener, which is governed by mTLS alone. What is exposed is then decided by how the host publishes the ports; `install.sh` publishes them on `127.0.0.1` only and that must stay so. |
| Cluster CA | On first start the Orchestrator generates a cluster-local CA (ECDSA P-256, 10 years) in `cluster-ca/` beside its database (dir `0700`, key `0600`, never leaves the manager). It is unrelated to the cloud CA. The trust bundle (`trust_bundle.pem`) can hold more than one CA so a rotation stays possible. |
| Cluster listener | Opened by the first `orcli nodes add`, never before. Built by `utils.ListenCluster`, which refuses to construct a listener whose mTLS side does not require and verify client certificates, so a non-loopback listener cannot exist without mTLS. One port, two sides told apart by ALPN during the handshake: **h2** (gRPC) needs a client certificate chaining to the cluster CA; **http/1.1** reaches only the join endpoint (`POST /v1/join`), which authenticates the server only because a joining node has no certificate yet, and can never reach gRPC. Certificates on the deny list are refused at the handshake and again on every RPC, so `nodes rm` also cuts connections that are already open. A member may only report its own containers and cannot create or list nodes. |
| Join | `orcli nodes add` prints one opaque `ulj_…` string: manager address + SHA-256 fingerprint of the cluster CA + a 256-bit single-use secret (15 minutes). The node verifies the manager's certificate chain against that fingerprint **during the TLS handshake, before sending anything**, so the secret is never offered to an impostor. The manager stores only the secret's hash, consumes it on first use, and answers a wrong, reused or expired secret identically. Node certificates: 730 days, `NotBefore` backdated 5 minutes, client-auth only, CN = node id (the CSR's other requests are ignored). Renewal starts 90 days before expiry over the live mTLS connection and is retried daily; a node whose certificate expired re-joins with a fresh string and keeps its id. `orcli nodes ls` shows each expiry and warns at 30 days; `orcli nodes rm` revokes every certificate of the node (deny list keyed by serial, pruned after the certificate would have expired anyway). |
| Agent link | Agents dial out; the Orchestrator never connects to an agent and has no address for one (`shared/clients.AgentDirectory` resolves a *node id* to its live link, nothing else). Commands travel down one gRPC bidirectional stream per node and results come back matched by request id; open-ended output (log follow) gets its own sub-stream, attached with a single-use token bound to the node it was issued for. On the **cluster listener** the node is the CN of the client certificate that passed mTLS; the hello cannot say otherwise, and `nodes rm` / re-enrollment drop the live link. On the **local listener** there is no credential (loopback only, like the rest of the local surface): the agent names its node, the node must be a `local`-transport node (the manager's own agent), and a link may *not* claim a node that joined through the cluster listener. A rogue local link can therefore only receive the local node's commands and answer them falsely or occupy its slot; it cannot make the Orchestrator or an agent do anything, which is the reverse of the old exposure, where any local process could drive the agent's Docker through `:50101`. Deployments are placed on a node by link (sticky per deployment; a down node is a clear error, never a silent move). |
| CLI sockets | `/tmp/undf-orch.sock` and `/tmp/undf-agent.sock` are created `0600`. The CLI refuses to dial a socket not owned by the current user, since requests can carry GitHub tokens. |
| Browser guards (REST) | Requests need a loopback `Host` (DNS rebinding), an allowlisted `Origin` if one is sent (CSRF), and `Content-Type: application/json` on POST/PUT/PATCH. The log WebSocket uses the same Origin allowlist. Extend with `UNDERLEAF_ALLOWED_ORIGINS` / `UNDERLEAF_ALLOWED_HOSTS` (comma-separated). |
| SQL | The `SqlQuery` RPC and `orcli sql` exist only in `-tags devmode` builds, and run on a read-only connection with `ATTACH` disabled. |
| Docker | See "Container mode". To let worker nodes reach a Docker-installed manager, publish the **cluster** port (`50104`) on the interface they will use; never publish `80`/`50100` beyond loopback. |

Not covered yet: REST authentication (needed before exposing the UI beyond localhost), and per-service container hardening (see `edge/agent/service/docker_service.go`).

## Running locally

From `edge/`:

```bash
make build-dev   # builds orcli, orch-server, migrate, ufagent, ufagentd into edge/bin
make run         # starts orch-server, ufagentd, and the Orchestrator UI via overmind
```

See [edge/Procfile](../Procfile) and [edge/Makefile](../Makefile) for the exact process definitions and build targets, and the [root README](../../README.md#development) for the full local dev workflow.
