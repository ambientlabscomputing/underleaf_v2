# cloud Nginx

Overview

```mermaid
flowchart LR
    N[Node]
    UI[Accounts UI]
    GW[Nginx Gateway]
    API[Cloud API]
    CON[Connections Worker]
    CCT[Connections Client]

    N -->|mTLS request| GW
    UI -->|Bearer token request| GW
    GW -->|API request| API
    API -->|gRPC control| CON
    CCT -->|HTTPS gateway request
     *.gw.underleafapp.com| GW
    GW -->|Gateway request| CON
```

Components:
- **Node**: a user's compute node (server, PC, Raspberry PI, etc)
- **Accounts UI**:  Our cloud-hosted React UI
- **Nginx gateway**: cloud fronting gatway
- **Cloud API**: Primary API
- **Conn Woker**: Microservice for handling connections and streams
- **Connections Client**: A user client accessing conn worker-hosted endpoints

## Detailed Flows

UI request

```mermaid
sequenceDiagram
    User->>UI: uses UI functionality
    UI->>Nginx: sends HTTPS request with Bearer token
    Nginx->>Nginx: terminates SSL
    Nginx->>CloudAPI: sends HTTP request with Bearer token
    CloudAPI->>CloudAPI: validates token and completes request
    CloudAPI->>Nginx: returns response
    Nginx->>UI: returns response
    UI->>User: displayes functionality result
```

Node HTTP + TLS request

```mermaid
sequenceDiagram
    Node->>Nginx: sends mTLS request
    Nginx->>Nginx: validates client cert and adds X-Subject-Id header to request
    Nginx->>API: sends http request with subject id header
    API->>API: generates a new token with the x subject id and uses it to complete request
    API->>Nginx: returns response
    Nginx->>Node: returns response
```

**Reference point for the next sequence diagrms**

An example of a gateway connection
```mermaid
flowchart LR
    CW[Conn Worker] -->|Northbound| N443[Nginx port 443]
    N443 ---|Northbound| UGC[User gateway client access at *.gw.underleafapp.com]
    CW -->|Southbound| NNode[Nginx southbound port 19021]
    NNode -->|Southbound| Node[Node connected via mTLS]
```

Start Stream request (Southbound / node-side)

```mermaid
sequenceDiagram
  User->>orcli: Runs "orcli streams new ..."
  orcli->>OrchServer: sends request
  OrchServer->>NginxGateway: sends HTTPS request requesting new connection and stream
  NginxGateway->>NginxGateway: validates client cert/terminates mTLS and add X-Subject-Id header
  NginxGateway->>CloudAPI: forward HTTP request
  CloudAPI->>ConnWorker: validate and forward request (gRPC)
  ConnWorker->>ConnWorker: generates new connection connections slot
  ConnWorker->>CloudAPI: returns new Connection and Stream
  CloudAPI->>OrchServer: persists data and returns new Connection and Stream
  OrchServer->>TargetNodeAgent: sends connection and stream request
  OrchServer->>TargetNodeAgent: starts polling for active stream
  TargetNodeAgent->>NginxGateway: sends start connection tcp request
  NginxGateway->>NginxGateway: validates client cert/terminates mTLS
  NginxGateway->>ConnWorker: forwards start connection tcp request to port 9021
  ConnWorker->>ConnWorker: validates connection against connection slot reserved earlier
  ConnWorker->>TargetNodeAgent: connection accepted
  TargetNodeAgent->>ConnWorker: starts new multiplexed stream over yamux connection
  TargetNodeAgent->>OrchServer: polling succeeds, stream is ready
  OrchServer->>orcli: returns success result
  orcli-->>User: displays success
```

Join Stream request (Southbound / client-side)

```mermaid
sequenceDiagram
  UserClient->>NginxGateway: naive* client sends HTTPS request to our gateway endpoint (https://<stream-id>.gw.underleafapp.com)
  NginxGateway->>NginxGateway: terminates SSL
  NginxGateway->>ConnWorker: forwards HTTP request to port 9020
  ConnWorker->>ConnWorker: matches request based on host prefix to corresponding stream (stream must be active or client will receive a gateway error)
  ConnWorker->>TargetNodeAgent: sends request down matched live stream
  TargetNodeAgent->>UserApp: forwards request to local target port
  UserApp->>TargetNodeAgent: returns app's response
  TargetNodeAgent->>ConnWorker: returns app's response
  ConnWorker->>UserClient: returns app's response
```

*naive: the user's client has no knowledge of the connection infrastructure--the gateway appears as an HTTP server just as any other, the Underleaf infrastructure should be largely invisible
