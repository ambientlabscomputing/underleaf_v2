# Conn Worker

## Architecture

```mermaid
flowchart TD
    subgraph Underleaf Cloud
        GW[Nginx Gateway]
        API[Cloud API]
        CW[Conn Worker]
    end
    subgraph User Cluster
        OS[Orchestrator Server]
        A[Agent]
        APP[User App]
    end
    U[User]

    U -->|Control-Request, local orcli| OS
    OS -->|Control-Request| GW
    GW -->|Control-Request| API
    API -->|Control-Response| GW
    API -->|Tunnel Configuration| CW
    GW -->|Control-Response| OS
    OS -->|Control-Response| A
    APP -->|App Traffic| A
    A -->|App Traffic| CW
    CW -->|App Traffic| GW
    GW -->|App Traffic, over internet| U
```

## API

### REST API

**Design intent** — intended for use by the Cloud API for management and data display. **Not implemented yet**: the actual REST server (`service/service.go`) currently only serves `/health`; none of the routes below exist.
```yaml
api:
    v1:
        tunnels:
            POST: Create a new tunnel
            GET: List + search tunnels
            {id}:
                GET: Get tunnel
                DELETE: Tear down tunnel
        metrics: # future
            performance:
                GET: performance related metrics
                timeseries:
                    GET: timeseries performance metrics
            tunnels:
                GET: tunnel metrics
                timeseries:
                    GET: timeseries tunnel metrics
                {id}:
                    GET: specific tunnel metrics
```

### gRPC

Used by the agent to establish and manage its connection with the Conn Worker. This part **is implemented** (yamux-multiplexed) — see `interface/grpc/*.proto` and [connection_manager.go](service/connection_manager.go). Actual service (the RPC names below differ from the design sketch above):

```proto
service ConnectionWorker {
    // Connections
    rpc CreateConnection(CreateConnectionRequest) returns (Connection);
    rpc GetConnection(GetConnectionRequest) returns (Connection);
    rpc TerminateConnection(TerminateConnectionRequest) returns (google.protobuf.Empty);
    rpc ListConnections(google.protobuf.Empty) returns (stream Connection);

    // Streams
    rpc NewStream(NewStreamRequest) returns (Stream);
    rpc CloseStream(CloseStreamRequest) returns (CloseStreamResponse);
    rpc ListStreams(google.protobuf.Empty) returns (stream Stream);
    rpc GetStream(GetStreamRequest) returns (Stream);
}
```

## Data Model

```mermaid
erDiagram
    ConnWorker ||--|| Connection: ConnectsTo
    Connection ||--o{ Stream : Multiplexes
    Agent ||--|| Connection: ConnectsTo
    Agent ||--o{ UserApp: Hosts
    UserApp }o--o{ Stream: OneStreamPerPort
```

| Info | Value |
| ---- | ----- |
| Language | Golang |
| Storage | Redis |

| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| gRPC | Agent connection/tunnel management | `:50102` |
| http | REST API for Cloud API management/data display, internal (currently just `/health` — see gap above) | `http://conn_worker:9070/api/v2/connections/` |
| http(s) | Northbound: gateway client access, per-stream subdomain (via nginx) | `https://<stream-id>.gw.underleafapp.com` → `conn_worker:9020` |
| tcp+mTLS | Southbound: node/agent tunnel establishment (via nginx stream proxy) | nginx `:19021` → `conn_worker:9021` |

## Running locally

```bash
make run
```

Normally run as part of the cloud process group (`cd cloud && make run`, or `make run-conn-worker`) — see the [root README](../../README.md#development). Config is read from `configs/local/conn_worker.yaml` (or `UNDERLEAF_CONFIG` when containerized via [docker-compose.yaml](../../docker-compose.yaml)); requires Redis.

