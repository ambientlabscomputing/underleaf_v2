# RFD-1: Key User Flows

### Onboarding

User flow

```mermaid
flowchart TD
    U([User Starts Onboarding]) --> s100[installs orchestrator-server
    + underleaf-agent on their
    management node]

    s100 --> s200[Runs `ufagent start

    --manager`]

    s200 --> s300[Visits newly available
    https://localhost:9090]

    s300 --> s400[User signs in with default
    login -- prompted to update
    password at thiis point]

    s400 --> s500[User now has access to the
    dashboard displaying the
    current cluster and node
    data]

    s500 --> END([Users's Underleaf 
    cluster is now set up])
```

System flow

```mermaid
sequenceDiagram
    User->>ufagent: User runs ufagent start --manager
    ufagent->>ufagentd: ufagent starts ufagentd
    ufagentd-->>ufagent: ufagentd reports health
    ufagent->>orchestrator-server: ufagent starts orchestrator-server
    orchestrator-server-->>ufagent: orchestrator-server reports health
    ufagent->>ufagentd: ufagent triggers ufagentd-orch server connection
    ufagentd->>orchestrator-server: establish connection witih orch server
    ufagentd-->>ufagent: ufagentd reports connection to orchestrator help health
    ufagent-->>User: ufagent reports results back to user
    User->>orchestrator-server: User can now access the UI and orcli
```
