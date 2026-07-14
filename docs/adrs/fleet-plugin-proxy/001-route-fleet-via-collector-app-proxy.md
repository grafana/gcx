# Route `fleet` and `instrumentation` through the `grafana-collector-app` plugin proxy

**Created**: 2026-06-24
**Updated**: 2026-07-07
**Status**: accepted
**Supersedes**: none

<!-- Status lifecycle: proposed -> accepted -> deprecated | superseded -->

## Context

`gcx fleet` and `gcx instrumentation` both talk to the Grafana Fleet Management
(FM) API **directly**. They share a base HTTP client (`internal/fleet/`) that
POSTs Connect-style JSON to the FM gRPC endpoint with Basic auth (the FM instance
ID as username, the FM access-policy token as password):

- `internal/fleet/` — the base client and its config loader, which resolves the
  FM instance URL/ID from GCOM stack info and the access-policy token from the
  active context.
- `internal/providers/fleet/` — typed pipeline/collector/tenant methods
  (`ListPipelines`, `Create/Update/DeletePipeline`, `*Collector`, `GetLimits`).
- `internal/providers/instrumentation/` — instrumentation, discovery, and
  pipeline methods layered on the same base client; sets the per-request
  `X-Prom-*` cluster/instance headers and embeds backend datasource push URLs in
  request bodies.

This direct path requires the caller to hold an **FM-scoped access-policy
token**, which gcx uses as the Basic-auth password (username = FM instance ID).

`grafana-collector-app` — the Grafana app plugin the Fleet Management UI already
uses — exposes the same FM routes under `/fleet-management-api/...` and relies on
**Grafana authNZ**. A caller authenticates to Grafana (`cfg.Host`) with its
existing credential and reaches FM without holding an FM token of its own.

**Why now.** FM's direct API doesn't accept Grafana OAuth credentials — it
requires a Cloud access-policy token — and we want OAuth to work across the board
in gcx. Routing through the plugin puts `fleet`/`instrumentation` on the same
Grafana-authenticated transport as every other provider.

The plugin-proxy transport is also the **dominant** HTTP pattern in gcx already:
assistant, aio11y, irm, k6, slo, and kg providers all reach the plugin proxy via
`rest.HTTPClientFor(&cfg.Config)` (`internal/assistant/assistanthttp/` is the
canonical template). Those providers target a *backend* plugin's
`/api/plugins/<id>/resources/...` endpoint. `grafana-collector-app` is a
**frontend-only** app plugin (it ships no backend), so it is reached through
Grafana's **app plugin-proxy** at `/api/plugin-proxy/grafana-collector-app/...` —
the same `rest.HTTPClientFor()` bearer transport at `cfg.Host`, only a different
path prefix. The config plumbing exists with no new wiring: the same
`providers.ConfigLoader` the fleet providers already instantiate exposes
`LoadGrafanaConfig(ctx) → config.NamespacedRESTConfig` (`Host` + `rest.Config`).

This change is **CONSTITUTION-touching**. `CONSTITUTION.md`'s dependency rule
mandates `httputils.NewDefaultClient(ctx)` (no auth injection) for "APIs outside
the Grafana server" and explicitly names **Fleet** as such a case. Routing through
the proxy moves these calls *to* `cfg.Host`, where the correct client is
`rest.HTTPClientFor()` (which injects the Grafana bearer token) — the inverse of
the current rule. The invariant is descriptive of today's design, not a
prohibition on changing it, but the amendment requires explicit sign-off.

### Route migration plan

Every RPC gcx calls today is reachable through the proxy. Role gating is coarse
and observable at the caller: **read** RPCs (the `List*` / `Get*` surfaces)
require the **Viewer** role, and every **mutation** — plus `tenant.v1 GetLimits`
and all `instrumentation.v1` / `discovery.v1` calls — requires the **Grafana
Admin** role. Finer per-resource authorization is a future plugin-side
enhancement (see Open Questions in the spec). We deliberately do not enumerate
the plugin's internal route configuration here — gcx describes only its own
calls and the role each requires.

The wire format is identical (the proxy is transparent), and backend datasource
URLs travel in the request **body**, so the instrumentation `Set*`/`Setup*`
paths pass through unchanged. gcx keeps populating those body URLs; only their
*source* moves — from a direct GCOM lookup to the collector-app's
Grafana-authenticated instance-metadata proxy route (Viewer role), so no Cloud
token is needed to build them.

### Alternatives considered

- **Keep direct access (status quo).** Self-contained and gcx-controlled, but
  requires every user/CI job to obtain and carry an FM-scoped access-policy token
  in addition to their Grafana credential.
- **Hybrid: plugin-proxy when the plugin is detected, else fall back to direct.**
  Rejected as unnecessary. Because the commands are Cloud-only and the plugin is
  guaranteed on all Cloud stacks, the fallback branch would serve no environment
  that exists for these commands. It would add a probe + two transports to
  maintain for a dead path. Revisit only if FM becomes reachable outside Grafana
  Cloud (i.e., a non-GCOM way to set the FM URL is introduced).

## Decision

Route all `gcx fleet` and `gcx instrumentation` FM traffic through the
`grafana-collector-app` plugin proxy, replacing direct FM access. Specifically:

1. **Transport.** Swap the `internal/fleet/` base client from the direct
   Basic-auth POST (to the FM endpoint) to the plugin-proxy pattern: build the
   HTTP client with `rest.HTTPClientFor(&cfg.Config)` and target
   `cfg.Host + /api/plugin-proxy/grafana-collector-app/fleet-management-api`
   + the existing service/method path. Grafana auth is injected by the k8s
   round-tripper. (The **app plugin-proxy** path `/api/plugin-proxy/<id>/...` is
   used — not the backend-plugin `/api/plugins/<id>/resources/...` endpoint —
   because `grafana-collector-app` ships no backend; the resources endpoint would
   not resolve for it.)
2. **Config.** Resolve connection details via
   `providers.ConfigLoader.LoadGrafanaConfig(ctx)` (`NamespacedRESTConfig`)
   instead of `LoadCloudConfig(ctx)`'s `AgentManagementInstanceURL` + FM token.
   gcx no longer needs an FM-scoped access-policy token for these commands, nor
   `AgentManagementInstanceURL`/`ID` from GCOM. `instrumentation` still needs the
   stack's backend datasource URLs (Mimir/Loki/Tempo/Pyroscope) in its request
   bodies, but sources them through the collector-app's Grafana-authenticated
   instance-metadata proxy route (**Viewer** role) rather than a direct GCOM
   call — so **no Cloud access-policy token is required for `instrumentation`
   either**. The cutover removes the Cloud-token requirement for both command
   groups outright; there is no residual dual-credential path.
3. **Drop now-redundant request decoration.** The instrumentation client no
   longer sets the `X-Prom-Cluster-ID`/`X-Prom-Instance-ID` (or `X-Scope-OrgID`)
   headers itself — the plugin proxy injects the cluster/instance and org-scope
   headers server-side, so client-side decoration is redundant. Backend
   datasource URLs remain in the request body (still gcx's responsibility, now
   sourced via the proxy per item 2).
4. **Clean cutover, no hybrid.** No fallback to direct FM access (see
   Alternatives).
5. **Amend the CONSTITUTION.** Update the dependency rule in `CONSTITUTION.md`
   to remove **Fleet** from the "outside the Grafana server" list and document
   that `fleet`/`instrumentation` reach FM via the collector-app proxy at
   `cfg.Host`, using `rest.HTTPClientFor()` like the other plugin-proxied
   providers.

Explicitly **rejected**: a hybrid plugin-or-direct transport; keeping the
FM-scoped token requirement.

## Consequences

### Positive

- **Enables OAuth support.** Direct FM access needs a Cloud access-policy token,
  so interactive OAuth users cannot drive `fleet`/`instrumentation` today.
  Because the proxy authenticates the caller to Grafana rather than to FM, any
  Grafana credential — OAuth included — now works. This holds for **both**
  command groups: routing `instrumentation`'s backend-URL lookup through the
  plugin proxy as well means neither `fleet` nor `instrumentation` keeps any
  Cloud-token dependency.
- **Consistency with the other providers.** `fleet`/`instrumentation` join the
  plugin-proxy transport family (assistant, aio11y, irm, k6, slo, kg) instead of
  being a Basic-auth special case. Behaviour converges too: errors, status
  codes, and auth all come from Grafana now, so these commands fail and
  authenticate the same way as the rest of gcx.
- **One less provider to require Cloud access-policy tokens.** gcx stops
  requiring an FM-scoped token for these commands and relies on the Grafana
  credential the active context already holds. Dropping the two-tier "Grafana
  credential *and* FM token" requirement removes real friction.
- **Opens the door to proper RBAC.** With traffic flowing through Grafana and the
  plugin, fine-grained authorization becomes achievable as a future enhancement
  (once the Fleet/Instrumentation teams implement RBAC in the plugin) rather than
  the all-or-nothing authority an FM token grants today.

### Negative / trade-offs

- **Breaking change.** This is a clean cutover with no hybrid fallback: callers
  that relied on direct FM access now authenticate to Grafana instead of carrying
  an FM token. Nothing else of note — the wire format, typed method surfaces, and
  request bodies are unchanged.

### Implementation plan

Once this ADR is accepted, realizing the decision involves the following work —
all in scope of executing this decision, not deferred beyond it:

- Implement the transport + config swap in `internal/fleet/` (base client) and
  adjust the `internal/providers/fleet/` and `internal/providers/instrumentation/`
  call sites; the per-method typed surfaces are unchanged. Target the app
  plugin-proxy path (`/api/plugin-proxy/grafana-collector-app/fleet-management-api`).
- Widen the `internal/fleet` `ConfigLoader` interface (today it declares
  `LoadCloudConfig` only) to expose `LoadGrafanaConfig`, and rethread the
  instrumentation commands typed against it.
- Add an instance-metadata fetch for `instrumentation`'s backend datasource URLs
  through the collector-app's Grafana-authenticated proxy route, reusing the
  existing `StackInfo` field mapping to build request bodies. Verify the proxied
  instance response carries the same fields `BackendURLsFromStack` consumes.
- Amend the `CONSTITUTION.md` dependency rule (remove **Fleet** from the "outside
  the Grafana server" list) to match this decision.
- Update `gcx fleet` / `gcx instrumentation` command docs to state the Grafana
  role requirement (Viewer for reads, `Admin` for mutations). Edit the Cobra
  `Short`/`Long` help and regenerate the CLI reference.
- Review error mapping for proxy/RBAC responses so messages stay actionable
  (e.g. distinguishing "missing RBAC" from "FM rejected the request").
- Update `ARCHITECTURE.md` (Instrumentation/Fleet sections + ADR index) and
  `internal/fleet` package docs to reflect the proxy transport.
- Reconcile tests in `internal/fleet`, `internal/providers/fleet`, and
  `internal/providers/instrumentation` (drop the `useBasicAuth`/instance-ID/token
  constructor args; auth assertions move from Basic-auth to plugin-proxy
  path/URL expectations).
- Respect the closed-source boundary: gcx is public, the collector-app plugin is
  not — public docs describe gcx's proxy calls without documenting plugin
  internals.

### Validation (2026-07-07)

Verified against the current `gcx` code and the `grafana-collector-app` plugin
before spec-out; two corrections were folded into this ADR:

- **Proxy path.** Transport targets the app plugin-proxy
  (`/api/plugin-proxy/grafana-collector-app/...`), not the backend-plugin
  resources endpoint (`/api/plugins/<id>/resources/...`) — the collector-app has
  no backend, so the resources path would not resolve.
- **`instrumentation` is fully OAuth-capable, no dual-config.** Its backend
  datasource URLs are sourced through the collector-app's Grafana-authenticated
  instance-metadata proxy route (Viewer role), so `instrumentation` keeps no
  Cloud-token dependency — matching `fleet`. The earlier assumption that gcx
  must reach GCOM directly (which would have left a residual token requirement)
  does not hold once that lookup is routed through the same proxy.

The `X-Prom-*`/`X-Scope-OrgID` header and FM-credential injection all occur
server-side in the proxy, confirming the client-side decoration is safe to drop.
