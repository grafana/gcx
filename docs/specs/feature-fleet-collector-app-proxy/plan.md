---
type: feature-plan
title: "Route `fleet` and `instrumentation` through the collector-app plugin proxy"
status: draft
spec: spec.md
created: 2026-07-07
---

# Architecture and Design Decisions

## Pipeline Architecture

Before — direct FM access (Basic auth, Cloud-token required):

```
gcx fleet / gcx instrumentation
        │
        ▼
providers.ConfigLoader.LoadCloudConfig(ctx)      ── requires cloud.token
        │                                            (loadCloudBase errors
        ▼                                             "cloud token is required")
GCOM GetStack(slug)
        │  AgentManagementInstanceURL + InstanceID
        │  + FM access-policy token
        │  + backend datasource URLs (Mimir/Loki/Tempo/Pyroscope)
        ▼
internal/fleet.Client
   NewClient(baseURL, instanceID, apiToken, useBasicAuth=true, httpClient)
   DoRequest: POST  {AgentManagementInstanceURL}{/service.method}
              Basic auth  instanceID:token
              X-Prom-Cluster-ID / X-Prom-Instance-ID (instrumentation)
        │
        ▼
   Fleet Management API (external domain)          ── OAuth users blocked
```

After — collector-app plugin proxy (Grafana auth, no Cloud token):

```
gcx fleet / gcx instrumentation
        │
        ▼
providers.ConfigLoader.LoadGrafanaConfig(ctx)     ── no cloud.token needed
        │  config.NamespacedRESTConfig (Host + rest.Config, Grafana bearer)
        ▼
internal/fleet.Client
   NewClient(cfg NamespacedRESTConfig)             ── rest.HTTPClientFor(&cfg.Config)
   DoRequest: POST  {cfg.Host}
                    /api/plugin-proxy/grafana-collector-app/fleet-management-api
                    {/service.method}              ── path suffix UNCHANGED
              bearer injected by k8s round-tripper
              (no Basic auth, no X-Prom-*/X-Scope-OrgID set client-side)
        │
        │   instrumentation backend URLs sourced via
        │   collector-app instance-metadata proxy route (Viewer) ── in request body
        ▼
   collector-app proxy  →  Fleet Management         ── any Grafana credential (OAuth)
   (reads = Viewer, mutations = Grafana Admin;
    proxy injects FM credential + X-Prom-*/X-Scope-OrgID server-side)
```

## Design Decisions

| Decision | Rationale |
|---|---|
| Rebuild `internal/fleet.Client` on `rest.HTTPClientFor(&cfg.Config)` targeting `cfg.Host + /api/plugin-proxy/grafana-collector-app/fleet-management-api` + the existing path suffix; drop `instanceID`/`apiToken`/`useBasicAuth`. | Moves transport to the Grafana-authenticated proxy so OAuth works; keeps the Connect path suffix stable. Traces FR-001/FR-002/FR-003. Files: `internal/fleet/client.go` (current `NewClient` L29, `DoRequestWithHeaders` L49-83, Basic auth L71-75). |
| Reuse the transport construction pattern from `internal/assistant/assistanthttp/client.go` (`NewClient` → `rest.HTTPClientFor`; `DoRequest` builds `cfg.Host + basePath + path`), but with the **app plugin-proxy** prefix `/api/plugin-proxy/grafana-collector-app/...` rather than the backend-plugin `/api/plugins/<id>/resources/...`. | assistanthttp is the canonical bearer-transport template, but its target plugin has a Go backend; collector-app is frontend-only, so the resources endpoint would not resolve. Traces FR-001. Files: `internal/assistant/assistanthttp/client.go` L20-58. |
| Widen the `internal/fleet.ConfigLoader` interface to add `LoadGrafanaConfig(ctx) (config.NamespacedRESTConfig, error)`; rework `LoadClient`/`LoadClientWithStack` to build the client from `NamespacedRESTConfig`. | The interface today declares only `LoadCloudConfig`; the Grafana-auth loader is the new dependency. Traces FR-004/FR-005. Files: `internal/fleet/config.go` (interface L15-17, `LoadClientWithStack` L43-68); loader `internal/providers/configloader.go` `LoadGrafanaConfig` L186. |
| Stop reading `AgentManagementInstanceURL`/`AgentManagementInstanceID` and the FM token; do not gate on them. | These come from `LoadCloudConfig`/GCOM and `loadCloudBase` hard-requires `cloud.token` — the exact OAuth blocker being removed. Traces FR-006/FR-011. Files: `internal/fleet/config.go` L49-64; `internal/providers/configloader.go` `loadCloudBase` L234-236, `LoadCloudConfig` L278. |
| Remove `PromHeaders`, `PromHeadersFromStack`, `toMap`, and the `PromHeaders` param threaded through `DoRequestWithHeaders` and the `cmd/gcx/instrumentation/**` call sites; keep `BackendURLs` in the body. | The proxy injects `X-Prom-*`/`X-Scope-OrgID` server-side, making client-side decoration vestigial; backend URLs stay gcx's responsibility. Traces FR-007/FR-008/FR-010. Files: `internal/providers/instrumentation/client.go` `PromHeaders` L91-110, `BackendURLs` L44-53, `BackendURLsFromStack` L58-69. |
| Add an instance-metadata fetch that sources backend datasource URLs through the collector-app's Grafana-authenticated instance-metadata proxy route (Viewer role), reusing the existing `StackInfo` → `BackendURLsFromStack` field mapping to build request bodies. | Removes the residual direct-GCOM/Cloud-token dependency, keeping instrumentation fully OAuth-capable. Traces FR-009/FR-011. Exact route path/response shape verified at build time (Open Question OQ-1). Files: `internal/providers/instrumentation/client.go` L44-69. |
| Keep the typed method surfaces and Connect service/method path suffixes byte-for-byte; only the base URL and auth change. | Preserves the public typed API and wire compatibility so only transport moves. Traces FR-012. Files: `internal/providers/instrumentation/client.go` path constants L17-26; `internal/providers/fleet/client.go` (`/pipeline.v1...`, `/collector.v1...`, `/tenant.v1.TenantService/GetLimits`). |
| Amend `CONSTITUTION.md`: remove **Fleet** from the `httputils.NewDefaultClient` "outside the Grafana server" rule and the Project Identity intro; document the proxy transport at `cfg.Host` via `rest.HTTPClientFor()`. | The traffic now targets `cfg.Host`, inverting the current rule; the invariant is descriptive, and the amendment requires explicit sign-off. Traces FR-013. Files: `CONSTITUTION.md` intro L10, dependency rule L161-168. |
| State the Viewer(read)/Grafana Admin(mutate) role requirement in the Cobra `Short`/`Long` help, then regenerate the CLI reference with `GCX_AGENT_MODE=false mise run reference`. | The reference docs are auto-generated from Cobra help; `mise run reference-drift` fails CI otherwise, and `GCX_AGENT_MODE=false` prevents agent-mode default flips. Traces FR-014/FR-015. Files: `internal/providers/fleet/provider.go` L104-116; `cmd/gcx/instrumentation/command.go` L37-38. |
| Map proxy/RBAC responses to actionable errors distinguishing a missing-role denial from an FM request rejection. | Proxy status codes are opaque without mapping; users need to know whether to acquire a role or fix the request. Traces FR-016. |
| Reconcile tests: replace `useBasicAuth`/instance-ID/token constructors, rewrite the Basic-auth assertion to expect the plugin-proxy path/URL + bearer transport, and augment `assertConnectRequest` for the new base path while keeping POST/json/path-suffix checks. | Tests currently encode the direct-FM contract; they must encode the proxy contract without loosening wire-format checks. Traces FR-017. Files: `internal/fleet/client_test.go` `TestNewClient_DoRequest_AuthHeaders` L17, `newTestClient`; `internal/providers/fleet/client_test.go` L15-17, `provider_test.go` L343; `internal/providers/instrumentation/client_test.go` L19-22, `assertConnectRequest` L52-58. |
| Single plugin-proxy transport; no hybrid/fallback branch. | Commands are Cloud-only and the plugin is guaranteed on all Cloud stacks, so a fallback path is dead code. Traces FR-018. |

## Compatibility

**Continues unchanged**

- Typed pipeline/collector/tenant/instrumentation/discovery method signatures and
  their Connect service/method path suffixes.
- The Connect JSON wire format of all request and response bodies, including the
  instrumentation backend datasource URLs carried in `Set*`/`Setup*` bodies.
- All other providers already on the plugin-proxy transport
  (assistant/aio11y/irm/k6/slo/kg) — this change adds fleet/instrumentation to
  that family without touching them.
- gcx config file schema and context model (the change reuses the existing
  `LoadGrafanaConfig` plumbing; no new config fields).

**Breaking change**

- Direct-FM callers that authenticated with an FM instance ID + FM-scoped Cloud
  access-policy token now authenticate to Grafana at `cfg.Host` instead. A context
  that carried only an FM/Cloud token — with no Grafana credential — will no longer
  work for fleet/instrumentation. Announce in `CHANGELOG.md` and add a doc callout
  on the affected commands.
- Mutations now require the Grafana Admin role; reads require Viewer. Callers
  whose credential lacks the needed role will be denied at the proxy.
- `cloud.token` / `GRAFANA_CLOUD_TOKEN` and `AgentManagementInstanceURL`/`ID` are
  no longer consulted for these command groups.

**Newly available**

- OAuth (and any Grafana credential) can now drive both `gcx fleet` and
  `gcx instrumentation` — no FM-scoped Cloud access-policy token required.
- The two-tier "Grafana credential *and* FM token" requirement is eliminated for
  these commands; a single Grafana credential suffices.
- fleet/instrumentation now fail, authenticate, and report errors consistently
  with the rest of gcx's plugin-proxied providers, and are positioned for
  fine-grained plugin-side RBAC as a future enhancement.
