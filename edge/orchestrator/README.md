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
    rest/           -- REST API for the Orchestrator UI (http://0.0.0.0:9090/api)
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
| gRPC | Agent-Orchestrator communication | `:50100` |
| http | REST API for the Orchestrator UI and integrations | `http://0.0.0.0:9090/api` |

## Running locally

From `edge/`:

```bash
make build-dev   # builds orcli, orch-server, migrate, ufagent, ufagentd into edge/bin
make run         # starts orch-server, ufagentd, and the Orchestrator UI via overmind
```

See [edge/Procfile](../Procfile) and [edge/Makefile](../Makefile) for the exact process definitions and build targets, and the [root README](../../README.md#development) for the full local dev workflow.
