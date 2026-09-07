# Agent

Runs on every node in a cluster (including the manager node, alongside the Orchestrator). Registers with the Orchestrator, hosts user apps/containers, and participates in the Agent-to-Agent network mesh and the Cloud Gateway tunnel. See the [root README](../../README.md) for how this fits into the overall architecture.

| Info | Value |
| ---- | ----- |
| Language | Golang |
| Architecture | CLI (`ufagent`) + Daemon (`ufagentd`) |
| Storage | Sqlite3 |

## Directory Structure

```
cmd/
    ufagent/   -- ufagent binary (operator/user CLI, limited surface)
    ufagentd/  -- ufagentd binary (long-running daemon)
interface/
    cli/            -- ufagent command implementations
    grpc_public/    -- Agent-Agent and Agent-Orchestrator gRPC (:50101)
    grpc_private/   -- internal daemon gRPC
    rest/           -- local REST surface
lib/
    conn_client/    -- client for the Cloud Gateway (conn_worker) tunnel connection
repository/
    migrations/     -- Sqlite schema migrations
service/            -- domain/business logic (containers, nodes, registration, streams)
```

## Interfaces

| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| gRPC | Unix socket management API for CLI | `unix:/tmp/undf-agent.sock` |
| gRPC | Agent-Orchestrator + Agent-Agent communication | `:50101` |

## Running locally

`ufagentd` is started as part of the edge process group; see [edge/Procfile](../Procfile):

```bash
cd edge
make build-dev   # builds ufagent, ufagentd (and the orchestrator binaries) into edge/bin
make run         # starts orch-server, ufagentd, and the Orchestrator UI via overmind
```

See the [root README](../../README.md#development) for the full local dev workflow.
