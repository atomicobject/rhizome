# Rhizome Web UI (React)

This directory contains the React/TypeScript source for the bundled Rhizome web UI.

## Build

```bash
cd web
npm install
npm run generate:api
npm run build
```

The typecheck scripts run the stable TypeScript 7 native compiler explicitly.
`typescript@5.9.3` remains a separate tooling dependency because
`openapi-typescript@7.13.0` still consumes the legacy TypeScript API and declares
compatibility with TypeScript 5 only; it is not used for application typechecks.

The OpenAPI source lives at `pkg/app/web/openapi.yaml` so the Go binary can embed API discovery alongside the UI assets. The build outputs to `pkg/app/web/assets/dist` so the Go binary can embed the assets.

## Dev Server

The default frontend iteration loop is:

```bash
make dev
```

That target:

- builds the backend binary once
- starts `rzm serve`
- waits for `.rhizome/serve-dev.json`
- starts `npm run dev`
- watches the built backend binary and restarts `rzm serve` when a new binary is written

The first backend launch uses a random port. Restarts reuse the last discovered
port via `rzm serve --reuse-discovery-port`, so Vite keeps talking to the same
API origin after `make build` or `make` in another terminal.

Split modes are available too:

```bash
make dev-backend
make dev-frontend
```

For UI iteration, run a normal Rhizome backend first, then point Vite at it:

```bash
rzm serve --vault .

cd web
npm run dev
```

When `npm run dev` handles an `/api` request, Vite walks up from `web/` to the
nearest `.rhizome/serve-dev.json` file and proxies to the current `rzm serve`
process. The target is resolved per request so backend restarts from `make dev`
keep working without Vite serving the app shell for API misses. Requests whose
host is `<token>.localhost` are proxied the same way regardless of path: that is
how HTML note viewer documents reach the backend when the browser is on Vite's
port. The proxy forwards `Host` and `Origin` unchanged so the backend sees the
Vite origin as the application origin.

If you need to override discovery, set `VITE_RHIZOME_API_ORIGIN` before
starting Vite:

```bash
VITE_RHIZOME_API_ORIGIN=http://127.0.0.1:8787 npm run dev
```

## Tests

```bash
cd web
npm run lint
npm run lint:fix
npm run typecheck
npm run typecheck:node
npm test
npm run test:e2e
```

- `npm run lint` runs Oxlint and `oxfmt --check`, failing on lint or formatting drift.
- `npm run lint:fix` applies Oxlint autofixes and reformats with oxfmt.
- `npm run typecheck` checks strict browser-side TypeScript.
- `npm run typecheck:node` checks Playwright config and local test harness scripts.
- `npm test` runs the Vitest unit suite for the extracted UI helpers/components.
- `npm run test:e2e` boots a temp copy of `testdata/integration/python-app/vault`,
  indexes it with `rzm index`, starts `rzm serve`, then drives the live UI
  with Playwright.

## Hooks

Run `make setup` once in this checkout to install the repo-owned
`.githooks/pre-commit` hook alongside the existing local Codex config setup. The
hook runs `make web-lint` whenever staged files under `web/` change, so commits
fail if oxfmt would reformat or Oxlint would report errors. Use `make web-fix` to apply
the fixes. If you only want to manage hooks, use `make hooks-setup` and
`make hooks-reset`.
