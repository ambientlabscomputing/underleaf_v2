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

Intended for use by the Cloud API for management and data display
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

Intended for use by the agent to establish and manage its connection with the Conn Worker

```proto
BeginConn(BeginConnRequest) returns BeginConnResponse;
TerminateConn(TerminateConnRequest) returns TerminateConnResponse;
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
