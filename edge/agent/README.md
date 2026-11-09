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
    grpc_public/    -- generated types of the command vocabulary + the AgentLink service (client side)
    grpc_private/   -- internal daemon gRPC (unix socket, CLI only)
link/               -- the agent link: dials out to the Orchestrator, runs its commands
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
| gRPC (outbound) | **Agent link** to the Orchestrator: one long-lived bidirectional stream, dialed by the agent | the Orchestrator's local listener (`127.0.0.1:50100`, single-node) or cluster listener (mTLS, after `ufagentd join`) |

**The agent listens on no TCP port.** Its only inbound surface is the owner-only unix socket above. The Orchestrator never connects to an agent: it sends commands (create/start/stop/remove container, volumes, logs, streams, ...) down the link the agent opened and the agent answers up it, matched by request id. If the link drops the agent reconnects with exponential backoff (0.5 s up to 15 s, with jitter), forever. `ufagentd join <string>` enrolls a node into a cluster (see the [Orchestrator README](../orchestrator/README.md#security-model)); without it the agent is the single-node install's agent and uses the loopback-only local listener.

## Running locally

`ufagentd` is started as part of the edge process group; see [edge/Procfile](../Procfile):

```bash
cd edge
make build-dev   # builds ufagent, ufagentd (and the orchestrator binaries) into edge/bin
make run         # starts orch-server, ufagentd, and the Orchestrator UI via overmind
```

See the [root README](../../README.md#development) for the full local dev workflow.
