# Grafana Cloud API Surface — Architectural Overview

> A tier-shaped mental model of the Grafana Cloud API surface, with the gcx
> package that maps to each tier. Read this before adding a new provider or
> extending an existing one.

## TL;DR

Grafana Cloud exposes **four tiers** of APIs, each with its own hostname,
auth model, and protocol. gcx maps one Go package to each tier. Knowing
which tier a feature lives in is the single most useful piece of context for
working on gcx or integrating with Grafana Cloud.

```
┌────────────────────────────────────────────────────────────────────────┐
│  Tier 1 — CONTROL PLANE (global)                                       │
│  grafana.com/api/...                                                   │
│  Stacks, orgs, access policies, tokens, plugin catalog, integrations   │
│  Auth: GCOM token                                                      │
├────────────────────────────────────────────────────────────────────────┤
│  Tier 2 — STACK (one hostname per tenant)                              │
│  <stack>.grafana.net/...                                               │
│  Dashboards, folders, users, alerting, datasources, plugins            │
│  Auth: service-account token / OAuth PKCE                              │
├────────────────────────────────────────────────────────────────────────┤
│  Tier 3 — REGIONAL RUNTIME (one hostname per product per region)       │
│  {prometheus,logs,tempo,profiles}-prod-NN.grafana.net/...              │
│  Signal stores (Mimir/Loki/Tempo/Pyroscope), ingest, product services  │
│  Auth: Basic (instance-id:token), product tokens                       │
├────────────────────────────────────────────────────────────────────────┤
│  Tier 4 — GLOBAL PRODUCT REST                                          │
│  api.k6.io                                                             │
│  Products whose data plane lives outside Grafana regions               │
│  Auth: product-specific token                                          │
└────────────────────────────────────────────────────────────────────────┘
```

## Tier 1 — Control plane (GCOM)

**Host**: `grafana.com/api/...` — one global surface, never regional.
**Auth**: GCOM token (Cloud Access Policy token).
**Role**: everything *about* a Grafana Cloud account that exists before or
above any individual stack.

```
/api/orgs/{slug}                          org + members
/api/stacks, /api/instances/{id}          stack lifecycle
/api/stack-regions                        region catalog
/api/v1/accesspolicies, /api/v1/tokens    IAM for Cloud itself
/api/plugins, /api/instances/{id}/plugins plugin catalog + install
/api/stacks/{id}/integrations             prebuilt integrations installer
/api/stacks/{id}/cloud-providers/...      AWS/GCP/Azure collectors
/api/instances/{id}/{hm,hl,ht,hp}         hosted-{metrics,logs,traces,profiles} mgmt
```

**gcx**: `internal/cloud/` (GCOM HTTP client + stack discovery).

## Tier 2 — Stack (per-tenant Grafana)

**Host**: `<stack>.grafana.net` — one per tenant, served by a Grafana instance.
**Auth**: service-account token or OAuth PKCE (`auth.grafana.net`).
**Role**: everything inside a single Grafana tenant. Has **five sub-surfaces**,
reflecting Grafana's 10-year evolution from REST to K8s-style to plugin-hosted:

| Sub-surface                      | Path                                | Purpose                                                                                                   |
| -------------------------------- | ----------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Legacy REST                      | `/api/...`                          | Oldest, broadest. Dashboards, users, teams, datasources, alerting, library panels, annotations, reports.  |
| K8s-style API (Grafana 12+)      | `/apis/<group>.grafana.app/...`     | 33+ groups. Kubeconfig-style auth via `k8s.io/client-go`. Future of all resources.                        |
| Plugin resources                 | `/api/plugins/<id>/resources/...`   | Per-app-plugin backends. Where Cloud products (SLO, SM, IRM, k6-shim) live.                               |
| Plugin proxy                     | `/api/plugin-proxy/<id>/...`        | Thin auth-passthrough to a plugin. Used by Fleet UI, App O11y, Faro CRUD.                                 |
| App routes + Live WebSocket      | `/a/<id>/...`, `/api/live/ws`       | UI deep-links, CLI proxies (Assistant), streaming.                                                        |

**gcx mapping**:

- `internal/resources/` + `cmd/gcx/resources/` → K8s-style API tier
- `internal/datasources/` + `cmd/gcx/datasources/` → `/api/datasources` + per-ds query clients
- `internal/providers/<product>/` → plugin-resources and plugin-proxy routes, one provider per product (alert, slo, synth, irm, faro, k6, sigil, kg, appo11y, fleet)
- `internal/assistant/` → `/a/<id>/...` CLI proxy + A2A SSE streaming

The K8s-style tier is where Grafana's architecture is heading. Legacy REST and
plugin-resources coexist indefinitely because many products haven't moved.

## Tier 3 — Regional runtime

**Host**: `<product>-prod-NN.grafana.net` — one dedicated hostname per product
per region (NN = region number, e.g. `prometheus-prod-36.grafana.net`).
**Auth**: Basic (instance-id:token); product tokens for SM/OnCall/Faro.
**Protocols**: REST dominates; Fleet Management and SM also speak gRPC/connect.
**Role**: the actual data plane — where signals are stored, queried, and
ingested, plus product services with latency-critical APIs.

```
prometheus-prod-NN          Mimir          query + push + ruler + aggregations
logs-prod-NN                Loki           query + push + adaptive_logs
tempo-prod-NN               Tempo          query + push + adaptive_traces
profiles-prod-NN            Pyroscope      query + connect services
alertmanager-prod-NN        Alertmanager   regional AM
otlp-gateway-prod-NN        OTLP           unified ingest
synthetic-monitoring-api-*  SM             REST + gRPC
fleet-management-prod-NN    Fleet          connect-go (collector.v1, pipeline.v1)
oncall-prod-NN              OnCall         public API
faro-api-*, faro-collector  Faro           API + RUM ingest
```

**GEM/GEL/GET admin** endpoints (`/admin/api/v1/tenants`) live on the same
hostnames — Enterprise-only tenant management.

**gcx mapping**:

- `internal/query/prometheus`, `internal/query/loki` → signal store queries
- `internal/auth/adaptive/` → shared Basic-auth-with-GCOM-cache transport used by all signal providers
- `internal/fleet/` → Fleet Management connect client
- `internal/providers/{metrics,logs,traces,profiles}/` → signal + adaptive commands
- `internal/providers/synth/`, `internal/providers/irm/` → regional REST clients

## Tier 4 — Global product REST

**Host**: non-`grafana.net` — typically the product's own hostname.
**Auth**: product-specific token.
**Role**: products whose backend predates or sits outside Grafana regions.

Currently just `api.k6.io`. The stack has a thin `k6-app` shim but all real
calls go to the global host.

**gcx**: `internal/providers/k6/` talks directly to `api.k6.io`.

## Cross-cutting concerns

### Authentication models

| Model                        | Used for                                                | gcx                                          |
| ---------------------------- | ------------------------------------------------------- | -------------------------------------------- |
| GCOM token                   | Control plane                                           | `internal/cloud/`                            |
| OAuth PKCE                   | Interactive stack login                                 | `internal/auth/` + `cmd/gcx/auth/`           |
| Service-account token        | Stack REST + K8s API                                    | `internal/config/` (rest.Config builder)     |
| Basic (instance-id:token)    | Regional runtime (Mimir/Loki/Tempo/Pyroscope, Fleet)    | `internal/auth/adaptive/`                    |
| Product tokens               | SM, OnCall, Faro, k6                                    | per-provider client                          |

### Hybrid products (multi-tier surfaces)

Some products emit calls across tiers. Knowing the map avoids wrong-tier fixes:

- **Alerting** — Tier 2 (legacy `/api/ruler` + K8s `rules.alerting.grafana.app`) + Tier 3 (regional ruler + AM)
- **Synthetic Monitoring** — Tier 2 (plugin settings) + Tier 3 (REST + gRPC) + Tier 1 (install/register)
- **Fleet Management** — Tier 1 (discovery) + Tier 3 (gRPC) + Tier 2 (plugin-proxy UI)
- **Frontend Observability (Faro)** — Tier 2 (plugin-proxy CRUD + sourcemaps) + Tier 3 (API + ingest)
- **IRM** — Tier 2 (plugin-resources for OnCall + Incidents) + Tier 3 (OnCall public) + legacy Tier 3 Incident
- **Adaptive {Metrics,Logs,Traces}** — Tier 2 (plugin UI) + Tier 3 (runtime config) + Tier 1 (enablement)

### Protocol mix

REST is the default. Notable non-REST:

- **K8s-style** — Tier 2 `/apis/*.grafana.app` — same HTTP surface `kubectl` uses.
- **gRPC / connect-go** — Fleet Management, Synthetic Monitoring.
- **WebSocket** — Grafana Live `/api/live/ws`.
- **SSE (A2A)** — Grafana Assistant streaming on top of plugin resources.

## One-page mental model

```
            ┌──────────────────────────┐
Interactive │  auth.grafana.net        │  OAuth PKCE
login       │  (OAuth)                 │
            └────────────┬─────────────┘
                         │ token
             ┌───────────▼──────────────┐
             │  Tier 1: GCOM            │  stack + policy + plugin + integration lifecycle
             │  grafana.com/api         │  (what you have)
             └───────────┬──────────────┘
                         │ names a stack
             ┌───────────▼──────────────┐
             │  Tier 2: Stack           │  everything inside the tenant
             │  <stack>.grafana.net     │  (what you configure)
             │  /api + /apis + plugins  │
             └───────────┬──────────────┘
                         │ names regional ingest/query URLs
             ┌───────────▼──────────────┐
             │  Tier 3: Regional        │  the signal data plane + product runtimes
             │  *-prod-NN.grafana.net   │  (what you send data to / query)
             └──────────────────────────┘

             ┌──────────────────────────┐
             │  Tier 4: Global product  │  k6
             │  api.k6.io               │
             └──────────────────────────┘
```

## Why this matters for gcx

1. **Provider boundaries match tier boundaries.** A new Cloud product provider
   almost always lives in Tier 2 (plugin resources) with optional Tier 3
   runtime calls. Never mix: one client per tier.
2. **Auth transport is tier-specific.** Don't reach for GCOM auth inside a
   Tier 3 provider — use `internal/auth/adaptive/`.
3. **The K8s-style tier is the target.** New resource types should prefer
   `*.grafana.app` over legacy REST where both exist.
4. **Gap analysis is tier-shaped.** The biggest gcx gaps today are:
   Tier 1 (stacks, access policies, tokens), slices of Tier 2 (full IAM,
   datasource CRUD, library panels, correlations), and Tier 1 integrations
   installers. See [migration-gap-analysis.md](migration-gap-analysis.md).
