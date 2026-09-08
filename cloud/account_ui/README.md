# Account Management UI

Cloud-hosted UI for account/billing management. Talks to the [Cloud API](../cloud_api/README.md). Follows [RFDs/UI-STANDARD.md](../../RFDs/UI-STANDARD.md) (reference implementation: [edge/orchestrator_ui](../../edge/orchestrator_ui)).

| Info | Value |
| ---- | ----- |
| Language | TypeScript |
| Framework | Vite + React |
| Datastore | TanStack Query |

| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| http | UI dev server | `http://localhost:5181/` |
| https | Production static build, served by nginx | `https://underleafapp.com/` |

## Running locally

```bash
npm install
npm run dev      # port 5181 (fixed via vite.config.ts, strictPort)
```

Talks to the Cloud API at `http://localhost:8080/api/v2/cloud` by default (override with `VITE_API_URL`; see `src/api/client.ts`). Normally run as part of the cloud process group (`cd cloud && make run`, or `make run-accounts-ui`) alongside the Cloud API — see the [root README](../../README.md#development). `npm run build` output (`dist/`) is what the nginx gateway container mounts and serves (see [cloud/docker/nginx/README.md](../docker/nginx/README.md)).
