# Cloud Cockpit UI

Premium multi-cluster management UI, for advanced users with more than one cluster or who pay for advanced remote-access features. Talks to the [Cloud API](../cloud_api/README.md). Follows [RFDs/UI-STANDARD.md](../../RFDs/UI-STANDARD.md) (reference implementation: [edge/orchestrator_ui](../../edge/orchestrator_ui)).

| Info | Value |
| ---- | ----- |
| Language | TypeScript |
| Framework | Vite + React |
| Datastore | TanStack Query |

| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| http | UI browser access | `http://0.0.0.0:9092/ui/` |

## Running locally

```bash
npm install
npm run dev      # standalone, port 5182
```

Normally run as part of the cloud process group (`cd cloud && make run`, or `make run-cockpit-ui`) alongside the Cloud API — see the [root README](../../README.md#development).
