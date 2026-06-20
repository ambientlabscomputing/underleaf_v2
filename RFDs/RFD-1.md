# RFD-1: Key User Flows

## Onboarding

User flow

```mermaid
flowchart TD

U([User Starts Onboarding])

U --> A[Install Underleaf]

A --> B[Underleaf configures this machine]

B --> C[Open local dashboard]

C --> D[Deploy first application]

D --> E([Underleaf is now useful])
```

System flow

```mermaid
sequenceDiagram

install-script->>system: install ufagent
install-script->>system: install ufagentd
install-script->>docker: pull underleaf image
install-script->>orchestrator: start orchestrator
install-script->>ufagentd: start daemon
install-script->>orchestrator: verify health
install-script-->>user: localhost:9090 ready
```

## Cluster Registration

User flow

```mermaid
flowchart TD
    U0([User Starts Cluster Registration])
    U0 --> A[User runs orcli cloud register]
    A --> B[orcli displays register URL (includes registration token)]
    B --> C[User visits URL]
    C --> D[User signs in/creates an account]
    D --> E[Confirmation panel appears for linking cluster to account]
    E --> F[User clicks Confirm]
    G --> H[User get redirected to clusters/{cluster_id}]
```

System Sequence (assumes brand new user, simpler case is existing user)

```mermaid
sequenceDiagram
user->>orcli: runs orcli cloud register
orcli->>orch-server: requests new registration URL
orch-server->>cloud-api: registers as ClusterCandidate
cloud-api->>orch-server: generates and returns device code
orch-server->>orch-server: starts polling for registration completion
orch-server->>orcli: constructs URL with device code and returns it
orcli->>user: displays URL and instructions

user->>accounts-ui: visits URL
accounts-ui->>cloud-api: authenticated request to backend with user token and cluster join token
cloud-api->>accounts-ui: links cluster and user and returns one time cluster token
cloud-api->>orch-server: polling complete, returns one time cluster token
orch-server->>cloud-api: uses one time token to get a signed cert from the cloud api root ca to establish mtls--all subsequent calls from cluster use mtls
```
