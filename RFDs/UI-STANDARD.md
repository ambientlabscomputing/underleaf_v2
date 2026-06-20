# UI Gold Standard

Reference: `edge/orchestrator_ui`. This document describes the patterns every UI in this repo must follow.

---

## Directory structure

```
src/
  main.tsx          ← provider bootstrap only
  App.tsx           ← route table only, no business logic
  api/
    client.ts       ← generic typed fetch wrapper + ApiError
    index.ts        ← (empty; services import client directly)
    services/
      Common.ts     ← shared base types (BaseQueryRequest, BaseQueryResponse)
      FooService.ts ← one file per domain resource
  datastore/
    queryClient.ts  ← QueryClient singleton with global defaults
    foo.ts          ← query-key factory + useXxx hooks for one domain
    index.ts        ← re-exports everything in datastore
  components/
    index.ts        ← re-exports everything; the only MUI surface outside this dir
    theme/
      theme.ts           ← createTheme (dark, Inter, palette tokens)
      AppThemeProvider.tsx
    PageShell.tsx   ← AppBar + collapsible Drawer layout wrapper
    AppDataGrid.tsx ← MUI DataGrid with project defaults + re-exported grid types
  pages/
    index.ts        ← re-exports all pages
    Foo.tsx         ← one file per route
```

---

## Layer rules

### `api/client.ts`
- Single `request<T>` function using `fetch`; throws typed `ApiError` on non-2xx.
- Exposes `apiClient` object: `get`, `post`, `put`, `patch`, `delete` — all generic, all typed.
- `BASE_URL` comes from `import.meta.env.VITE_API_URL`.

### `api/services/FooService.ts`
- Plain object (not a class), named `fooService`.
- Owns all types for that domain: interfaces are co-located and exported from the same file.
- Calls `apiClient` and unwraps response shapes (`.then(r => r.results)`).
- No state, no side-effects, no hooks.
- Shared pagination types live in `Common.ts` and are extended via `extends BaseQueryRequest`.

### `datastore/foo.ts`
- One file per domain, mirrors the service.
- Exports a `fooKeys` key factory (`all`, `list`, `byId`, etc.) as a `const` object with typed tuples.
- Exports named `useFoo` hooks that call `useQuery` / `useMutation` and delegate to the service.
- For streaming/websocket resources: custom hook with `useState` + `useRef` + `useEffect`, not React Query.
- `datastore/index.ts` re-exports every hook and key factory — pages never import from individual datastore files.

### `components/`
- **MUI is only allowed inside `components/`**. Pages and datastore never import from `@mui/*` directly.
- Every MUI type that consumers need (e.g. `GridColDef`) is re-exported from `components/index.ts`.
- Wrapper components set project-wide defaults (e.g. `AppDataGrid` sets `density="compact"`, `pageSizeOptions`, custom empty/loading overlays).
- `AppThemeProvider` wraps `MuiThemeProvider` + `CssBaseline`; theme is defined in `theme.ts` with named palette tokens.
- Components are thin layout/display wrappers — no data fetching.

### `pages/Foo.tsx`
- Imports come from `../components` and `../datastore` only.
- Column/config constants (e.g. `GridColDef[]`) are defined as **top-level module constants**, not inside the component.
- No direct calls to `apiClient` or any service.
- Each page owns its nav item list as a local `const`.
- Default export is **not** used — all exports are named.

### `App.tsx`
- Route table only: `<Routes>` + `<Route>` elements.
- Contains a catch-all redirect to the default route.
- No data fetching, no state, no business logic.

### `main.tsx`
Provider stack order (outer → inner):
```
StrictMode
  BrowserRouter
    AppThemeProvider
      QueryClientProvider (with queryClient from datastore)
        App
        ReactQueryDevtools
```

---

## Naming conventions

| Thing | Convention |
|---|---|
| Service object | `fooService` (camelCase, singular) |
| Query key factory | `fooKeys` |
| Datastore hook | `useFoo`, `useFooById`, `useCreateFoo` |
| Page component | `PascalCase`, matches route noun (`Home`, `Containers`, `ContainerLogs`) |
| Shared component | `PascalCase`, prefixed `App` when it wraps a library primitive (`AppDataGrid`) |
| Service types | Co-located in service file, exported; imported by pages via `import type` |

---

## Code style

- Sections within a file (top → bottom): Types → Constants → Helpers → Component/Hook.
- No default exports in components or pages (named exports only).
- `import type` for all type-only imports.
- `staleTime: 30_000`, `retry: 1` as QueryClient defaults; override per-query only when justified.

---

## What does NOT belong here

- Zustand, Redux, or any global mutable store — server state lives in React Query; local UI state uses `useState`.
- Direct `fetch` calls outside `api/client.ts`.
- MUI imports in pages, datastore, or `App.tsx`.
- Business logic in `App.tsx` or `main.tsx`.
- Default exports for components or pages.
