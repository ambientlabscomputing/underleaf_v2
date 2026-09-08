# Orchestrator UI

Operator UI for managing a single cluster: containers, nodes, streams, etc. Talks to the [Orchestrator](../orchestrator/README.md) REST API. This is the **reference implementation** for [RFDs/UI-STANDARD.md](../../RFDs/UI-STANDARD.md) — every other UI in this repo follows its patterns.

| Info | Value |
| ---- | ----- |
| Language | TypeScript |
| Framework | Vite + React |
| Datastore | TanStack Query |

| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| http | UI dev server | `http://localhost:5183/` |

This is a standalone Vite dev server/SPA build — the Orchestrator does not serve it; there's no production static-hosting path wired up for it yet (unlike Account UI, which nginx serves in production).

## Running locally

```bash
npm install
npm run dev      # port 5183 (fixed via vite.config.ts, strictPort)
```

Talks to the Orchestrator REST API at `http://localhost:9090/api/v1` by default (override with `VITE_API_URL`; see `src/api/client.ts`). Normally run as part of the edge process group (`cd edge && make run`) alongside the Orchestrator — see the [root README](../../README.md#development).

## Structure

See [RFDs/UI-STANDARD.md](../../RFDs/UI-STANDARD.md) for the required directory layout, layer rules, and naming conventions (`api/`, `datastore/`, `components/`, `pages/`).
