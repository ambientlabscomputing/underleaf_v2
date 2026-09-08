# Cloud Cockpit UI

Premium multi-cluster management UI, for advanced users with more than one cluster or who pay for advanced remote-access features.

**Unimplemented.** This is currently just the unmodified `create-vite` scaffold — no `api/`, `datastore/`, `components/`, or `pages/` layers, no TanStack Query dependency, no connection to the Cloud API. When work starts here it should follow [RFDs/UI-STANDARD.md](../../RFDs/UI-STANDARD.md) (reference implementation: [edge/orchestrator_ui](../../edge/orchestrator_ui)), the way `account_ui` already does.

| Info | Value |
| ---- | ----- |
| Language | TypeScript |
| Framework | Vite + React |

| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| http | UI dev server | `http://localhost:5182/` |

## Running locally

```bash
npm install
npm run dev      # port 5182 (fixed via vite.config.ts, strictPort)
```

Normally run as part of the cloud process group (`cd cloud && make run`, or `make run-cockpit-ui`) — see the [root README](../../README.md#development).
