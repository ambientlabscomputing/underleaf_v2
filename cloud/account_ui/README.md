# Account Management UI

Cloud-hosted UI for account/billing management. Talks to the [Cloud API](../cloud_api/README.md). Follows [RFDs/UI-STANDARD.md](../../RFDs/UI-STANDARD.md) (reference implementation: [edge/orchestrator_ui](../../edge/orchestrator_ui)).

| Info | Value |
| ---- | ----- |
| Language | TypeScript |
| Framework | Vite + React |
| Datastore | TanStack Query |

| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| http | UI browser access | `http://0.0.0.0:9091/ui/` |

## Running locally

```bash
npm install
npm run dev      # standalone, port 5181
```

Normally run as part of the cloud process group (`cd cloud && make run`, or `make run-accounts-ui`) alongside the Cloud API — see the [root README](../../README.md#development). `npm run build` output feeds the nginx gateway container (see [cloud/docker/nginx/README.md](../docker/nginx/README.md)).
