# Orchestrator UI

Operator UI for managing a single cluster: containers, nodes, streams, etc. Talks to the [Orchestrator](../orchestrator/README.md) REST API. This is the **reference implementation** for [RFDs/UI-STANDARD.md](../../RFDs/UI-STANDARD.md) — every other UI in this repo follows its patterns.

| Info | Value |
| ---- | ----- |
| Language | TypeScript |
| Framework | Vite + React |
| Datastore | TanStack Query |

| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| http | UI dev server (proxies `/api` to the Orchestrator) | `http://localhost:5183/` |
| http | UI as served by the Orchestrator | `http://127.0.0.1:9090/` (same origin as `/api/v1`) |

In production the [Orchestrator](../orchestrator/README.md) serves the built UI (`npm run build` -> `dist/`, found via `UNDERLEAF_UI_DIR`) and the REST API from **one listener on one origin**: no CORS, one port. `install.sh` downloads the built UI next to the Orchestrator and sets `UNDERLEAF_UI_DIR` for it; there is no separate web server.

## Running locally

```bash
npm install
npm run dev      # port 5183 (fixed via vite.config.ts, strictPort)
```

The dev server is same-origin too: `vite.config.ts` proxies `/api` (including the log WebSockets) to the Orchestrator at `http://127.0.0.1:9090`; override with `ORCHESTRATOR_URL=http://127.0.0.1:<port> npm run dev`. The API base is relative (`/api/v1`); `VITE_API_URL` overrides it (then the target must allow your origin via `UNDERLEAF_ALLOWED_ORIGINS`, since there is no CORS). Normally run as part of the edge process group (`cd edge && make run`) alongside the Orchestrator — see the [root README](../../README.md#development).

## Structure

See [RFDs/UI-STANDARD.md](../../RFDs/UI-STANDARD.md) for the required directory layout, layer rules, and naming conventions (`api/`, `datastore/`, `components/`, `pages/`).
