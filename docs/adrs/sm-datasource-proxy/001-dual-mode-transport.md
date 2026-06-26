# Synthetic Monitoring dual-mode transport: datasource proxy primary, direct SM API fallback

**Created**: 2026-06-16
**Status**: accepted
**Supersedes**: none

## Context

The `gcx synthetic-monitoring` provider talks to the Synthetic Monitoring (SM)
API. Historically the typed clients (`internal/providers/synth/{checks,probes}`)
called the SM API **directly** (`https://synthetic-monitoring-api-…/api/v1/…`)
with an SM "CAP" publisher token in the `Authorization` header. That token is
obtained out-of-band: either set explicitly (`sm-token`/env), or auto-discovered
via the SM `register/install` endpoint using a Grafana Cloud access-policy token
(`cloud.token`) plus GCOM stack info.

This created two problems:

1. **The SM CAP token requirement.** Every caller had to hold (or be able to
   mint) an SM-specific token, separate from their Grafana credential. OAuth
   (`gat_`) and SAT callers already authenticate to Grafana; forcing a second
   token is friction and, for pure-OAuth contexts with no `cloud.token`, the
   `register/install` path cannot mint one at all.
2. **Auth-path divergence.** SM was the only provider whose typed write surface
   bypassed the caller's Grafana credential entirely.

Grafana's datasource proxy offers a better path. The `grafana-synthetic-monitoring-app`
plugin defines an `sm` proxy route for all four verbs
(`GET`/`POST`/`PUT`/`DELETE`) forwarding to `{{.JsonData.apiHost}}/api/v1/`, with
`reqAction` `grafana-synthetic-monitoring-app:{read,write}`. The route injects
the SM token **server-side** via `Authorization: Bearer {{.SecureJsonData.accessToken}}`,
so the client only needs its Grafana credential and the SM datasource UID. The
proxy enforces RBAC, which is desirable.

The proxy path is **not** universally available, however: a Grafana stack whose
OAuth client lacks the SM plugin scope returns `403 plugin proxy route access
denied` (verified on prod OAuth, where the `gcx-prod.libsonnet` permission diff
was never applied). Cutting hard to the proxy would regress those contexts and
any token-only / headless / CI usage.

## Decision

**Synth becomes dual-mode: datasource proxy primary, direct SM API fallback.**

- A new transport `internal/query/synth` builds
  `/api/datasources/proxy/uid/<uid>/sm/<path>` requests with `rest.HTTPClientFor`
  (carrying the caller's Grafana credential) and returns non-2xx responses as
  data (`*Response{StatusCode, Body}`), not errors, so callers can inspect status
  and decide to fall back.
- The typed clients (`checks.Client`, `probes.Client`) gain a constructor
  `NewClient(restCfg, datasourceUID, fallback)`:
  - When `datasourceUID` is non-empty, requests go through the proxy.
  - On a proxy **403** — or when `datasourceUID` is empty (no SM datasource
    resolvable) — requests fall back to the direct SM API via
    `httputils.NewDefaultClient` + an SM token resolved **lazily** (once, via
    `LoadSMConfig`) only when the fallback is first taken.
- The loader gains `LoadSMProxyConfig(ctx) → (restCfg, datasourceUID, namespace,
  err)` = `LoadGrafanaConfig` + `dsquery.ResolveAndSaveDatasource(kind=
  "synthetic-monitoring")`. It returns an **empty UID, not an error**, when the
  proxy is unavailable, so the typed clients degrade to direct rather than
  failing.

### Fallback trigger is 403-only

Fall back on `403` (the verified, unambiguous proxy-access-denial signal)
**only**. A `404` is treated as a real SM response (e.g. `check/<id>` not found →
`ErrNotFound`), because an SM-level not-found and a proxy route-not-found share
the same status and cannot be distinguished by status alone — and route-404 risk
is near-zero (all `sm` routes have existed in the plugin since early 2025). `5xx`
also does not trigger fallback; surfacing the upstream error is more honest than
masking it and doubling requests on flakes.

### The direct SM API path is permanent, not a bridge

The fallback is kept indefinitely, not as a migration crutch. Token-based access
is genuinely preferred in CI/headless environments where minting and holding an
SM token is simpler than an interactive Grafana credential. Keeping it also means
the migration never regresses a context that worked before.

### User-facing surface is unchanged

The canonical surface stays `gcx synthetic-monitoring checks|probes …`. No new
`gcx datasources synthetic-monitoring` subcommand is introduced.

## Consequences

- OAuth/SAT callers on stacks with the SM plugin scope no longer need an SM CAP
  token — their Grafana credential suffices via the proxy.
- Contexts without proxy access (e.g. prod OAuth missing the libsonnet scope, or
  token-only/CI) transparently fall back to the direct SM API, provided an SM
  token is resolvable. Pure-OAuth contexts with no `cloud.token` still cannot
  mint one via `register/install`; for them, only the prod libsonnet diff (a
  separate `deployment_tools` change, out of scope here) closes the gap.
- Synth is now the one provider that legitimately uses **both** `rest.HTTPClientFor`
  (proxy, `cfg.Host`) and `httputils.NewDefaultClient` (direct, external domain).
  This is a documented carve-out to the CONSTITUTION "external APIs use httputils"
  invariant; both usages remain consistent with that rule's `cfg.Host`-vs-external
  logic.
- `register/install` token acquisition stays on the direct/fallback path; it is
  not proxied (only `register/save` and `register/viewer-token` are).

## Alternatives considered

- **Hard cut to the proxy (drop direct SM API).** Rejected: regresses prod-OAuth
  and token-only/CI contexts, and discards a transport the user wants to keep.
- **Fall back on 403 *and* 404 (the originally-sketched trigger).** Rejected:
  conflates SM not-found with proxy route-not-found, corrupting `ErrNotFound`
  semantics; route-404 risk is negligible.
- **Decide proxy-vs-direct once upfront by probing.** Rejected: an extra
  round-trip per command and racy; per-request 403 fallback is simpler and
  handles the scope-gap case directly.
