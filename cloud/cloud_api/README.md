# Cloud API

## Data Model

```mermaid
erDiagram
    User }|--|| PrincipalAccount : HasAccessTo
    PrincipalAccount ||--o{ Clusters : Owns
    PrincipalAccount ||--o{ Tunnels : Owns
    Tunnels ||--o{ Connections : Contains
    PrincipalAccount ||--|| BillingAccount : Contains
    BillingAccount ||--|| EntitlementsBucket : Manages
    BillingAccount ||--o{ UsageEvents : Tracks
    BillingAccount ||--o{ CreditEvents : Tracks
    Clusters ||--o{ Nodes : Contains
    Nodes ||--|| Tunnels : ConnectsTo
    BillingAccount ||--|| Subscription : Contains
```

| Info | Value |
| ---- | ----- |
| Language | Python |
| Framework | FastAPI |
| Storage | Postgres |

| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| http | REST API, internal (behind nginx) | `http://cloud_api:8080/api/v2/cloud/` (`http://localhost:8080/...` outside Docker) |
| https | REST API, external (via nginx, Bearer token or mTLS) | `https://underleafapp.com/api/v2/cloud/` |

## Running locally

```bash
make run
```

Normally run as part of the cloud process group (`cd cloud && make run`, or `make run-cloud-api`) — see the [root README](../../README.md#development). Config is read from `configs/local/cloud_api.yaml` (or `UNDERLEAF_CONFIG` when containerized via [docker-compose.yaml](../../docker-compose.yaml)). Schema migrations live under `alembic/`.

