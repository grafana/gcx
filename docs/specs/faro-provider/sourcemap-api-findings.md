# Faro Sourcemap API Findings

**Date**: 2026-04-03
**Context**: During Faro provider migration (PR #343), sourcemap commands hit 500s on dev/ops instances.

## Problem

The gcx CLI (`gcli faro sourcemaps`) uses the Grafana plugin proxy for all sourcemap operations:
```
/api/plugins/grafana-kowalski-app/resources/api/v1/app/{id}/sourcemaps
```

This endpoint returns 500 (`plugin.requestFailureError`) on all apps across dev and ops contexts.
Server-side plugin logging is sparse — no useful diagnostics found.

## Investigation

Explored two repos:
- **grafana/app-o11y-kwl** — the Faro Grafana plugin
- **grafana/faro-javascript-bundler-plugins** — the faro-cli / bundler plugins

### Finding: Two Separate APIs

**1. Plugin Proxy API** (UI-facing, listing + deletion)
```
GET/DELETE  /api/plugin-proxy/grafana-kowalski-app/api-proxy/api/v1/app/{id}/sourcemaps
Auth: Standard Grafana SA token (via plugin proxy)
Used by: Grafana UI (sourcemaps tab in Faro app settings)
```

**2. Direct Upload API** (build-time, used by bundler plugins)
```
POST  {grafana_stack_url}/api/v1/app/{appId}/sourcemaps/{bundleId}
Auth: Bearer {stackId}:{apiKey}  ← NOT a standard Grafana SA token
Used by: faro-cli, Webpack/Rollup/Vite bundler plugins at build time
Source: faro-javascript-bundler-plugins/packages/faro-bundlers-shared/src/index.ts
```

### Upload Details (from faro-bundlers-shared)

- **bundleId**: Generated at build time (timestamp + random string)
- **Content-Type**: `application/json` (individual) or `application/gzip` (batch tar.gz)
- **Auth format**: `Bearer {stackId}:{apiKey}` — stackId is the Grafana Cloud stack ID, apiKey is a Cloud Access Policy token
- **Endpoint construction**: `{endpoint}/api/v1/app/{appId}/sourcemaps/{bundleId}`

### Current gcx Implementation

Our `apply-sourcemap` command currently routes through the plugin proxy, which is wrong.
The `show-sourcemaps` and `remove-sourcemap` commands also use the plugin proxy, which
may or may not work (currently 500 — unclear if plugin bug or unsupported endpoint).

## Recommended Follow-Up

1. **Move `apply-sourcemap` to the direct upload API**
   - New base path: `{stack_url}/api/v1/app/{appId}/sourcemaps/{bundleId}`
   - Auth: `Bearer {stackId}:{apiKey}` — needs ConfigKeys or can derive from existing context
   - Generate bundleId client-side (match faro-cli pattern)
   - Support both individual files and tar.gz batches

2. **Keep `show-sourcemaps` and `remove-sourcemap` on plugin proxy**
   - These are listing/deletion operations the UI uses
   - The 500 may be a plugin bug or misconfiguration — file an issue against app-o11y-kwl
   - When it works, our implementation is correct

3. **Auth model change for upload**
   - The direct API uses `Bearer {stackId}:{apiKey}` format
   - stackId: available from `cfg.Namespace` (parsed from `stacks-{id}`)
   - apiKey: the existing Grafana token should work (it's a Cloud Access Policy token)
   - May need to construct the auth header differently than `rest.HTTPClientFor` provides

4. **File a bug against app-o11y-kwl**
   - Plugin proxy sourcemaps endpoint returns 500
   - Sparse logging makes debugging impossible
   - Request: better error messages or at minimum debug-level logging in the plugin backend
