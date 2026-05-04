# SM datasource query support — findings (2026-05-04)

Companion to [`2026-05-04-sm-datasource.md`](./2026-05-04-sm-datasource.md).
Mem's doc covers the *code* gap (dispatcher switch). This one covers the
*auth* gap that determines whether closing the dispatcher gap is enough.

## Goal

Make SM data (probes, checks, etc.) reachable from gcx without a
separate auth mechanism for OAuth-only users.

## Three paths to SM data; two backends behind them

| Path                                                | Backend                            | Today |
|-----------------------------------------------------|------------------------------------|-------|
| `gcx datasources query <uid> ...`                   | Grafana DS proxy                   | Not wired (no SM arm in dispatcher) |
| `gcx api /api/datasources/proxy/uid/<uid>/sm/...`   | Grafana DS proxy                   | **403** for OAuth tokens — plugin-proxy RBAC denies |
| `gcx synth probes\|checks ...`                      | SM REST API direct                 | Works with CAP token; no OAuth path |
| `gcx resources get ...syntheticmonitoring...`       | Same as `synth` (provider adapter) | Same constraints as `gcx synth` |

The K8s `resources` route does **not** hit `/apis` for SM despite the
schema being registered — the adapter dispatches to the synth provider.
Verified with `-vvv`: registration log shows
`registering provider adapter gvk="syntheticmonitoring.ext.grafana.app/v1alpha1, Kind=Check"`
followed by the same `SM token not configured` error as `gcx synth`.

## Auth picture

- OAuth (assistant proxy) is plumbed through `/apis` only; legacy
  `/api/datasources/proxy/...` reaches Grafana but Grafana RBAC rejects
  the SM plugin-proxy hop with 403 for current OAuth scopes
  (`grafana-api:read/write/delete`).
- The synth provider (`internal/providers/synth/`) calls the SM REST
  API directly using a CAP token (`cloud.token`) or
  `GRAFANA_PROVIDER_SYNTH_SM_TOKEN`.

## Implication for mem's options

- **Option A (DS proxy via dispatcher):** doesn't unblock OAuth users
  unassisted — needs either (a) server-side scope expansion to permit
  the SM plugin-proxy route, or (b) redirect to the synth provider
  (which collapses A into B).
- **Option B (provider tier):** already works with a CAP token. Gap is
  cosmetic — `gcx synth` and `gcx resources get` both reach SM today.
- **Real decision** isn't query dispatcher placement; it's how OAuth
  users authenticate to SM. Pick that first.

## Open questions

- Can the assistant-proxy / Grafana RBAC be extended so OAuth scopes
  permit `grafana-synthetic-monitoring-app` plugin-proxy routes?
- Is there appetite for the synth provider to accept an OAuth bearer
  (rerouting through the OAuth proxy at `/api/v1/...`) instead of CAP?
