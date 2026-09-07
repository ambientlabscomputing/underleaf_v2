# Orchestrator UI

Operator UI for managing a single cluster: containers, nodes, streams, etc. Talks to the [Orchestrator](../orchestrator/README.md) REST API. This is the **reference implementation** for [RFDs/UI-STANDARD.md](../../RFDs/UI-STANDARD.md) — every other UI in this repo follows its patterns.

| Info | Value |
| ---- | ----- |
| Language | TypeScript |
| Framework | Vite + React |
| Datastore | TanStack Query |

| Protocol | Purpose | Location |
| -------- | ------- | -------- |
| http | UI browser access | `http://0.0.0.0:9090/ui/` |

## Running locally

```bash
npm install
npm run dev      # standalone, port 5183
```

Normally run as part of the edge process group (`cd edge && make run`) alongside the Orchestrator it talks to — see the [root README](../../README.md#development).

## Structure

See [RFDs/UI-STANDARD.md](../../RFDs/UI-STANDARD.md) for the required directory layout, layer rules, and naming conventions (`api/`, `datastore/`, `components/`, `pages/`).
