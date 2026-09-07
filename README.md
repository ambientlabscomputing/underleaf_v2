# Underleaf V2

Underleaf V1 itself went through a large number of fundamental refactors, so why V2 now? Because during the previous changes, we were improving to fit a certain product shape. V2 marks the start of a new product shape.

V1: Large, complex microervice cloud backend; fragmented, multi-agent client; cloud-connection required
- this resulted is a ton of split brain bugs and made everything much more complex for very little gain

V2: Truly local, unified management servicy; small simpe flexible client; cloud API/UI is for billing/admin and network gateways

## What Stays

We keep the repo-backed manifest model: `ufctl deploy gh:<user>/<repo>` is still the flagship command.

Cloud Gateway Features: we keep our network mesh model.

Users don't pay to remove artificial paywalls, they pay for usage on cost-usage features (cloud gateway)

## What Leaves

Orchestration in the cloud

Long running connections to the cloud.

Microservices

## What's New

Premium Cloud Cockpit UI: the local Orchestrator UI, API and CLI interfaces are fully powered. The Cockpit UI is for advanced users that have more than one cluster or want to pay for advanced remote accesss features.

## TODO

- [ ] Flesh out connection worker
    - [ ] implement real tunnels (yamux) mutiplexing connections
- [ ] Flesh out cluster-cloud mTLS
- [ ] App manifest features

## Architecture

```mermaid
graph TD
    subgraph Underleaf Cloud
        CAPI[Cloud API]
        CGW[Cloud Gateway]
        ACC[Account Management UI]
        CCU[Cloud Cockpit UI]
    end


    subgraph Cluster
        subgraph Manager Node
            ORC[Orchestrator]
            OUI[Orchestrator UI]
            AG0[Agent]
        end

        subgraph Worker Node A
            AG1[Agent]
        end
        subgraph Worker Node B
            AG2[Agent]
        end
    end

    User

    ORC <--> OUI
    AG0 ==> ORC
    AG1 ==> ORC
    AG2 ==> ORC
    AG0 <-->|Network Mesh| AG1
    AG0 <-->|Network Mesh| AG2
    AG2 <-->|Network Mesh| AG1
    AG0 -->|Network Mesh| CGW
    AG1 -->|Network Mesh| CGW
    AG2 -->|Network Mesh| CGW
    ORC --> CAPI
    ACC --> CAPI
    CAPI --> CGW
    CCU --> CAPI

    User --> CCU
    User --> ACC
    User --> OUI
    User -->|CLI| ORC
    User -->|Agent CLI -- Limited| AG0
    User -->|Agent CLI -- Limited| AG1
    User -->|Agent CLI -- Limited| AG2
```

### Manager Node
#### Orchestrator

Orchestrator is the main brain of the system. It orchestratos all of the Underleaf automations necessary to make the Underleaf manifest model work. This is the main control entry point for a cluster. One Orchestrator per Cluster.

| Info | Value |
| ---- | ----- |
| Language | Golang |
| Architecture | Hexagonal Domain Driven Design App |
| Storage | Sqlite3 |

Interfaces


| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| gRPC | Unix socket management API for CLI | unix:/tmp/undf-orch.sock |
| gRPC | Agent-Orchestrator communication | :50100
| http | REST API for UI and integrations | http://0.0.0.0:9090/api

#### Orchestrator UI

Operator UI for managing your cluster

| Info | Value |
| ---- | ----- |
| Language | TypeScript |
| Framework | Vite + React |
| Datastore | TanStack |

Interfaces

| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| http | UI browser access | http://0.0.0.0:9090/ui/ |


### All Nodes

#### Agent

| Info | Value |
| ---- | ----- |
| Language | Golang |
| Architecture | CLI + Daemon |
| Storage | Sqlite3 |

Interfaces

| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| gRPC | Unix socket management API for CLI |  unix:/tmp/undf-agent.sock |
| gRPC | Agent-Orchestrator + Agent-Agent communication | :50101


### Underleaf Cloud

#### Cloud API


| Info | Value |
| ---- | ----- |
| Language | Python |
| Framework | FastAPI |
| Architecture | Hexagonal Domain Driven Design App |
| Storage | Postgres |

Interfaces

| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| http | UI browser access | http://0.0.0.0:9091/api/ |


#### Conn Worker

Implements the Cloud Gateway: brokers and multiplexes tunnels between Agents and gateway clients.

| Info | Value |
| ---- | ----- |
| Language | Golang |
| Storage | Redis |

Interfaces

| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| gRPC | Agent connection/tunnel management | :50102 |
| http | REST API for Cloud API management/data display | see [conn_worker README](cloud/conn_worker/README.md) |

See [cloud/conn_worker/README.md](cloud/conn_worker/README.md) for the detailed connection/tunnel architecture and data model.

#### Account Management UI

Account Management UI

| Info | Value |
| ---- | ----- |
| Language | TypeScript |
| Framework | Vite + React |
| Datastore | TanStack |

Interfaces

| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| http | UI browser access | http://0.0.0.0:9091/ui/ |


#### Cloud Cockpit UI

Premium multi-cluster management UI for advanced users

| Info | Value |
| ---- | ----- |
| Language | TypeScript |
| Framework | Vite + React |
| Datastore | TanStack |

Interfaces

| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| http | UI browser access | http://0.0.0.0:9092/ui/ |

## Directory Structure

```
README.md (You are here)
Makefile (root Makefile: make run / make stop everything)
go.mod (single Go module covering edge/ and cloud/conn_worker)
RFDs/ (design docs -- key user flows, connection architecture, UI gold standard)
configs/local/ (per-service YAML config for local dev + docker-compose)
certs/ (local dev TLS material, gitignored)
e2e_testing/ (pytest-based end-to-end tests against the running stack)
shared/ (Go: types, clients, migrator, and utils shared by orchestrator/agent/conn_worker)
scripts/ (dev helper scripts, e.g. kill_by_port.sh)
cloud/
    |-Procfile, Makefile (overmind targets for cloud services)
    |-cloud_api/       (Python/FastAPI: Cloud API, pyproject.toml, src/cloud_api)
    |-conn_worker/     (Go: Cloud Gateway connection/tunnel broker)
    |-account_ui/      (TypeScript/Vite/React: Account Management UI)
    |-cockpit_ui/      (TypeScript/Vite/React: Cloud Cockpit UI)
    |-docker/          (Dockerfiles for cloud_api, conn_worker, nginx gateway config)
edge/
    |-Procfile, Makefile (overmind targets for edge services)
    |-orchestrator/    (Go: orchestrator server, CLI (orcli), migrate binary; uses the root go.mod)
    |-agent/           (Go: ufagent CLI + ufagentd daemon)
    |-orchestrator_ui/ (TypeScript/Vite/React: Orchestrator UI)
```

## Development

Each of `edge/` and `cloud/` is a [Procfile](https://devcenter.heroku.com/articles/procfile)-driven service group, run locally via [`overmind`](https://github.com/DarthSim/overmind).

```bash
# from repo root: build edge binaries (dev-mode), then start both edge + cloud groups
make run
# tear everything down (overmind, node, conn-worker processes)
make stop
```

- `edge/Procfile` runs `orch-server`, `ufagentd`, and the Orchestrator UI (`npm run dev`, port 5183).
- `cloud/Procfile` runs `cloud_api` (`make run` inside `cloud/cloud_api`), `conn_worker`, and the Account (`5181`) and Cockpit (`5182`) UIs.
- Per-group targets exist too: `cd edge && make run` / `cd cloud && make run`, or run a single service, e.g. `cd cloud && make run-cloud-api`.
- `make build-orc` / `cd edge && make build` compiles the Go binaries (`orcli`, `orch-server`, `migrate`, `ufagent`, `ufagentd`) into `edge/bin`.
- `docker-compose.yaml` + `configs/local/*.yaml` stand up a more production-like Cloud stack (nginx gateway, cloud_api, conn_worker, postgres, redis) as containers instead of local processes.
- End-to-end tests live in [`e2e_testing/`](e2e_testing/README.md) (pytest) and expect the stack above to be running.
- UI code (`account_ui`, `cockpit_ui`, `orchestrator_ui`) should follow [RFDs/UI-STANDARD.md](RFDs/UI-STANDARD.md) — `edge/orchestrator_ui` is the reference implementation.

## Tagging Policy

Main branch is `develop`

### Edge

On push to develop:
- build new "develop" release
    - build new binaries (ufagent, ufagentd and orcli)
    - overwrite "develop" release with new release
    - add binaries to this new release
- build new "develop" docker image
    - build new Docker image tagged `underleaf:develop`
    - push to `ghcr.io` (overwrite existing if any)

On push to SemVer tag:
- build new release for tag:
    - build new binaries (ufagent, ufagentd and orcli)
    - create new "[0-9].[0-9].[0-9]" release
    - overwrite "latest" release with new release
    - add binaries to these new releases
- build new "latest", "[0-9]", "[0-9].[0-9]", "[0.9].[0-9][0-9]" docker images
    - for example, if we push the tag `2.3.40`, we would build:
        - `underleaf:latest`
        - `underleaf:2`
        - `underleaf:2.3`
        - `underleaf:2.3.40`
    - push to `ghcr.io` (overwrite existing if any)
