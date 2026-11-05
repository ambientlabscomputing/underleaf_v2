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
| gRPC | Agent-Orchestrator communication, **local listener** (single-node install; unauthenticated, loopback only) | `127.0.0.1:50100` |
| gRPC (mTLS) + HTTPS join | **Cluster listener**: worker nodes. Exists only after `orcli nodes add`; client certificates from the cluster CA are required | `0.0.0.0:50104` (advertised address is what nodes dial) |
| http | REST API (JSON only, no static assets) for the Orchestrator UI and integrations | `http://127.0.0.1:9090/api/v1/` |

Routes today: nodes, containers, logs, deployments, `/health` (see `interface/rest/*.go`). CORS and the Origin guard allow `http://localhost:5173` and `http://localhost:5183`.

## Security model

The orchestrator has **no per-user authentication** on the REST API, the Agent gRPC, or the CLI socket. Anyone who can reach a listener can deploy workloads (which run on the agent's Docker daemon), read container logs, and open metered streams. The trust boundary is therefore the local host, and the controls below keep it there.

| Control | Behavior |
| ------- | -------- |
| Loopback bind | All TCP listeners (REST `:9090`, agent gRPC `:50100`/`:50101`, agent REST) bind `127.0.0.1`. `UNDERLEAF_BIND_ADDR` overrides it. |
| Fail-closed remote bind | A non-loopback bind panics at startup unless the listener is protected by `UNDERLEAF_CLUSTER_TOKEN` (gRPC only) or you set `UNDERLEAF_INSECURE_ALLOW_REMOTE=1`. The REST API cannot be token-protected today, so it always needs the explicit opt-in. |
| Cluster token | If `UNDERLEAF_CLUSTER_TOKEN` is set on both sides, every agent↔orchestrator gRPC call must carry it (constant-time check, unary and streaming). The link is still plaintext, so the token gates access but doesn't stop a network sniffer. Use it with a trusted network until mTLS lands. |
| Cluster CA | On first start the Orchestrator generates a cluster-local CA (ECDSA P-256, 10 years) in `cluster-ca/` beside its database (dir `0700`, key `0600`, never leaves the manager). It is unrelated to the cloud CA. The trust bundle (`trust_bundle.pem`) can hold more than one CA so a rotation stays possible. |
| Cluster listener | Opened by the first `orcli nodes add`, never before. Built by `utils.ListenCluster`, which refuses to construct a listener whose mTLS side does not require and verify client certificates, so a non-loopback listener cannot exist without mTLS. One port, two sides told apart by ALPN during the handshake: **h2** (gRPC) needs a client certificate chaining to the cluster CA; **http/1.1** reaches only the join endpoint (`POST /v1/join`), which authenticates the server only because a joining node has no certificate yet, and can never reach gRPC. Certificates on the deny list are refused at the handshake and again on every RPC, so `nodes rm` also cuts connections that are already open. A member may only report its own containers and cannot create or list nodes. |
| Join | `orcli nodes add` prints one opaque `ulj_…` string: manager address + SHA-256 fingerprint of the cluster CA + a 256-bit single-use secret (15 minutes). The node verifies the manager's certificate chain against that fingerprint **during the TLS handshake, before sending anything**, so the secret is never offered to an impostor. The manager stores only the secret's hash, consumes it on first use, and answers a wrong, reused or expired secret identically. Node certificates: 730 days, `NotBefore` backdated 5 minutes, client-auth only, CN = node id (the CSR's other requests are ignored). Renewal starts 90 days before expiry over the live mTLS connection and is retried daily; a node whose certificate expired re-joins with a fresh string and keeps its id. `orcli nodes ls` shows each expiry and warns at 30 days; `orcli nodes rm` revokes every certificate of the node (deny list keyed by serial, pruned after the certificate would have expired anyway). |
| CLI sockets | `/tmp/undf-orch.sock` and `/tmp/undf-agent.sock` are created `0600`. The CLI refuses to dial a socket not owned by the current user, since requests can carry GitHub tokens. |
| Browser guards (REST) | Requests need a loopback `Host` (DNS rebinding), an allowlisted `Origin` if one is sent (CSRF), and `Content-Type: application/json` on POST/PUT/PATCH. The log WebSocket uses the same Origin allowlist. Extend with `UNDERLEAF_ALLOWED_ORIGINS` / `UNDERLEAF_ALLOWED_HOSTS` (comma-separated). |
| SQL | The `SqlQuery` RPC and `orcli sql` exist only in `-tags devmode` builds, and run on a read-only connection with `ATTACH` disabled. |
| Docker | The image binds all interfaces inside the container (required for port publishing) and sets the insecure opt-in; `install.sh` publishes the ports on `127.0.0.1` only. Keep it that way, or set a cluster token and drop the opt-in. |

Not covered yet: REST authentication (needed before exposing the UI beyond localhost), the agent link over the cluster listener (the Orchestrator still dials the agent until RFD 5 step 5), and per-service container hardening (see `edge/agent/service/docker_service.go`).

## Running locally

From `edge/`:

```bash
make build-dev   # builds orcli, orch-server, migrate, ufagent, ufagentd into edge/bin
make run         # starts orch-server, ufagentd, and the Orchestrator UI via overmind
```

See [edge/Procfile](../Procfile) and [edge/Makefile](../Makefile) for the exact process definitions and build targets, and the [root README](../../README.md#development) for the full local dev workflow.
