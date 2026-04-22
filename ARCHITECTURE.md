# Architecture: gcx

## Vision

See [VISION.md](VISION.md) for goals, roadmap, and product surface.

**In brief:** A CLI for managing Grafana and Grafana Cloud. Supports dynamic Grafana API resources via a kubectl-like resources layer, and per-product features via the provider interface. Includes observability-as-code workflows (`gcx dev`), multi-stack configuration/contexts, and Grafana Assistant integration. Optimized for AI agents and human use.

## System Overview

### 1. Resources Pipeline

The core of gcx. Manages Grafana-native resources (dashboards, folders, alert rules, etc.) with Grafana's Kubernetes-compatible `/apis` endpoint (available in Grafana 12 or later).

```
User input                           gcx resources push ./dashboards/
    |
    v
Selector (partial)                   "dashboards/" or "dashboards/my-dash"
    |
    v
Discovery Registry                   API call to /apis → available GVKs
    |
    v
Filter (resolved)                    Full GVK: dashboard.grafana.app/v1alpha1
    |
    v
Processors                           Strip server fields (pull) / add namespace (push)
    |
    v
Dynamic Client (k8s.io/client-go)   Create-or-update via /apis endpoint
    |
    v
Grafana K8s API                      /apis/{group}/{version}/namespaces/{ns}/{plural}/{name}
```

**Operations:** `get`, `push` (create-or-update, idempotent), `pull` (export to local YAML/JSON), `delete`, `edit` (single resource, `$EDITOR`), `validate` (local linting via Rego), `schemas` (discover types), `examples` (show sample manifests).

**Key abstractions** ([resource-model.md](docs/architecture/resource-model.md)): `Resource` wraps `unstructured.Unstructured` — no pre-generated Go types. `Selector` → `Filter` two-stage resolution keeps CLI ignorant of API details. `Processor` pipeline composes transformations at defined pipeline points. `Discovery` registry resolves plural names and short names to full GVKs at runtime.

**Data flows** ([data-flows.md](docs/architecture/data-flows.md)): Push reads local files, resolves selectors, applies processors, pushes via dynamic client with folder-before-dashboard ordering and bounded concurrency (errgroup, default 10). Pull fetches from API, strips server-managed fields, writes to disk grouped by kind.

### 2. Provider System

Pluggable adapters for Grafana Cloud products. Each provider is a self-contained package under `internal/providers/` that contributes CLI commands and optionally bridges into the resources pipeline.

```
Provider (internal/providers/slo/)
    |
    +-- Commands()            Cobra commands: gcx slo definitions list
    |
    +-- TypedRegistrations()  Adapter registrations for resources pipeline
    |       |
    |       v
    |   adapter.Register()    Makes provider resources accessible via gcx resources get/push/pull
    |
    +-- ConfigKeys()          Declares provider-specific config keys (token, url, ...)
    |
    +-- Validate()            Validates config before API calls
```

**TypedCRUD\[T\]** bridges typed Go domain structs to K8s-style `unstructured.Unstructured` envelopes. Domain types implement `ResourceIdentity` (`GetResourceName`/`SetResourceName`). `TypedObject[T]` wraps them with `ObjectMeta` + `TypeMeta` for K8s compliance.

**ConfigLoader** (`providers.ConfigLoader`) handles `--config`/`--context` flag binding, YAML + env var precedence, and provider-specific config resolution (`GRAFANA_PROVIDER_{NAME}_{KEY}`). All providers must use it — no ad-hoc `os.Getenv`.

**Dual access paths** are permanent: provider commands (`gcx slo definitions list`) give ergonomic domain-specific tables; generic commands (`gcx resources get slos.v1alpha1.slo.ext.grafana.app`) serve the push/pull pipeline. JSON/YAML output is identical across both paths by construction (both use the same `ResourceAdapter`).

**Deep-dive:** [patterns.md](docs/architecture/patterns.md) [§11 (Provider Plugin System)](docs/architecture/patterns.md#11-provider-plugin-system), [§17 (K8s Envelope Wrapping)](docs/architecture/patterns.md#17-k8s-envelope-wrapping-for-provider-listget), [§18 (Table-Driven TypedCRUD)](docs/architecture/patterns.md#18-table-driven-typedcrud-registration-for-providers), [§19 (Singleton Adapter)](docs/architecture/patterns.md#19-singleton-adapter-pattern), [§20 (ETag-as-Annotation)](docs/architecture/patterns.md#20-etag-as-annotation-pattern). Implementation guide: [provider-guide.md](docs/reference/provider-guide.md).

### 3. Signal Providers

Top-level commands for querying observability datasources: `metrics`, `logs`, `traces`, `profiles`. These bypass the K8s dynamic client and call datasource HTTP APIs directly.

```
gcx metrics query -d prom-001 'rate(http_requests_total[5m])' --since 1h
    |
    v
SharedOpts                   Shared flags: -d/--datasource, --from, --to, --since, --step
    |
    v
Datasource Resolution        Resolves -d flag to datasource UID (by name, UID, or config default)
    |
    v
Query Client                 internal/query/prometheus/ or internal/query/loki/ (direct HTTP)
    |
    v
Codec Pipeline               table (default) | graph (terminal chart) | json | yaml
```

**Standardized verbs**: `query` (execute queries), `labels` (list label names/values), `series`/`metrics` (list series or compute metric queries), `metadata` (metric metadata). All four signal providers share these verbs with identical flag semantics.

**Dual command mounting**: Datasource commands are accessible via two paths — top-level signal commands (`gcx metrics query`) and the `datasources` subgroup (`gcx datasources prometheus query`). Both paths call the same exported command constructors in `internal/datasources/{kind}/`. The signal-path mount lives in the signal provider's `Commands()` (e.g. `internal/providers/metrics/provider.go`). The `datasources`-path mount is driven by the `DatasourceProvider` interface (`internal/datasources/provider.go`) and its registry (`internal/datasources/registry.go`); built-in `DatasourceProvider` implementations self-register via `init()` in `internal/datasources/providers/` (blank-imported from `cmd/gcx/root/command.go`).

**Adaptive telemetry** nests under each signal provider (`metrics adaptive`, `logs adaptive`, `traces adaptive`) with its own CRUD resources (rules, policies, exemptions, segments) and operational views (recommendations, patterns). Uses `internal/auth/adaptive/` for shared GCOM-cached Basic auth.

**Graph rendering:** `internal/graph/` converts query responses to terminal charts via ntcharts + lipgloss. Available as `-o graph` on all query commands and SLO/synth timeline commands.

### 4. Developer Tooling (`gcx dev`)

Observability-as-code workflows for managing Grafana resources as typed Go code via [grafana-foundation-sdk](https://github.com/grafana/grafana-foundation-sdk). The `gcx dev` commands produce and validate resources that feed into the standard `gcx resources` pipeline.

**End-to-end workflow:** `scaffold` → `import`/`add` → edit Go code → `serve`/`lint` → build to manifests → `resources push`

- **`scaffold`** — Generate a new project (Go module + foundation-sdk + folder structure)
- **`import`** — Import existing dashboards/alerts from Grafana as Go builder code
- **`serve`** — Live-reload dev server (Chi router, reverse proxy, WebSocket reload) — edit code, preview in browser
- **`lint`** — Lint resources with built-in and custom Rego rules (OPA engine in `internal/linter/`), including PromQL/LogQL expression validators
- **`generate`** — Code generation utilities

The linter engine is also used by `gcx resources validate` for pre-push validation. See [VISION.md § Observability as Code](VISION.md#observability-as-code) for the full workflow vision.

### 5. Setup (`gcx setup`)

Onboarding area for stack-level concerns (connection, auth). Not a provider — standalone command area.

- **`setup status`** — Check connection, auth, and product availability

> Instrumentation used to live here as `gcx setup instrumentation` (ADR-014). It was promoted to a top-level provider under `gcx instrumentation` in ADR-016 — see [§ 2 Provider System](#2-provider-system) and `internal/providers/instrumentation/`.

### 6. Configuration

kubectl-inspired context-based multi-environment configuration.

```yaml
current-context: prod
contexts:
  prod:
    grafana: { server: https://grafana.example.com, token: gf_... }
    cloud: { token: glsa_..., org: my-org }
    providers:
      slo: { token: glsa_... }
      synth: { sm-url: https://... }
```

**Loading chain:** Config file → env var overrides (`GRAFANA_SERVER`, `GRAFANA_TOKEN`, `GRAFANA_PROVIDER_{NAME}_{KEY}`) → CLI flags (`--context`). Env vars take precedence over YAML. The `--context` flag selects the active context; absent, `current-context` is used.

**Namespace resolution:** `org-id` (on-prem, maps to K8s namespace) or `stack-id` (Cloud, discovered via GCOM). Providers use `ConfigLoader` which resolves these uniformly.

**Secret handling:** Config keys marked `Secret: true` in provider `ConfigKeys()` are redacted in `gcx config view`. Undeclared keys and unknown providers are redacted by default (secure-by-default).

**Deep-dive:** [config-system.md](docs/architecture/config-system.md).

### 7. Authentication

Multiple auth mechanisms for different tiers.

| Mechanism | Used for | Implementation |
|-----------|---------|----------------|
| **Service account token** | Grafana K8s API (`/apis`), plugin APIs | Bearer token in `rest.Config` |
| **Cloud Access Policy token** | GCOM stack discovery, Cloud product APIs | `internal/cloud/` GCOM client |
| **OAuth PKCE** | Browser-based login (`gcx login`) | `internal/auth/` — token refresh transport persists to config |
| **Basic auth** | Legacy Grafana instances | Username/password in `rest.Config` |
| **Adaptive auth** | Signal provider adaptive telemetry APIs | `internal/auth/adaptive/` — GCOM-cached Basic auth shared across signal providers |

**Precedence:** Token > OAuth > user/password. Explicit flags override env vars override config file. `httputils.NewDefaultClient(ctx)` must be used for APIs outside the Grafana server (k6 Cloud, OnCall, Synth, Fleet) — the k8s transport injects the Grafana bearer token on every request, which conflicts with product-specific auth.

**Deep-dive:** [client-api-layer.md](docs/architecture/client-api-layer.md), [config-system.md](docs/architecture/config-system.md).

## Architecture Decision Records

| ADR | Title | Status |
|-----|-------|--------|
| [001](docs/adrs/legacy/001-query-under-datasources.md) | Move query under datasources with per-kind subcommands | accepted |
| [002](docs/adrs/adapter-schema-example/001-align-examples-with-schemas-ux.md) | Align `resources examples` with `resources schemas` UX | accepted |
| [003](docs/adrs/cloud-rest-config/001-cloud-config-and-gcom.md) | CloudConfig in Context and GCOM Stack Discovery | accepted |
| [004](docs/adrs/config-layering/001-multi-file-config-layering.md) | Multi-File Config Layering (System/User/Local) | accepted |
| [005](docs/adrs/constitution-design-principles/001-codify-cli-design-principles.md) | Codify CLI Design Principles in CONSTITUTION.md and Design Guide | accepted |
| [006](docs/adrs/conventional-commits/001-pr-title-enforcement.md) | Conventional Commits via PR Title Enforcement | accepted |
| [007](docs/adrs/provider-consolidation/001-consolidation-strategy.md) | Provider Consolidation Strategy | accepted |
| [008](docs/adrs/typed-resource-adapter-compliance/001-typed-resource-adapter-foundation.md) | TypedResourceAdapter[T] with ResourceIdentity and Provider Command Migration | proposed |
| [009](docs/adrs/migrate-provider-rewrite/001-three-stage-blackbox-verification.md) | Three-Stage Skill Structure with Dual Blackbox Isolation | superseded by [012] |
| [010](docs/adrs/oncall-typed-crud/001-table-driven-typedcrud.md) | Table-driven TypedCRUD[T] for OnCall Adapter | proposed |
| [011](docs/adrs/adaptive-provider/001-cli-ux-and-resource-adapter-design.md) | Adaptive telemetry provider: CLI UX, adapter scope, verb naming | proposed |
| [012](docs/adrs/migrate-provider-rewrite/002-five-phase-pipeline-redesign.md) | Five-phase pipeline redesign for /migrate-provider | accepted |
| [013](docs/adrs/appo11y-provider/001-cli-ux-and-resource-adapter-design.md) | App O11y provider: singleton TypedCRUD, ETag-as-annotation, verb naming | accepted |
| [014](docs/adrs/instrumentation/001-instrumentation-provider-design.md) | Declarative Instrumentation Setup under `gcx setup` | superseded by [016] |
| [015](docs/adrs/faro-provider/001-faro-provider-design.md) | Faro provider: CLI UX, TypedCRUD adapter, sourcemaps as sub-resource verbs | proposed |
| [016](docs/adrs/instrumentation/002-top-level-provider.md) | Instrumentation as a top-level provider with decomposed `Cluster` + `App` kinds | accepted |

See [docs/adrs/](docs/adrs/) for all ADRs.

## Architecture Docs

Deep-dive docs live in [docs/architecture/](docs/architecture/). Each covers one domain:

| Document | Domain | When to Read |
|----------|--------|--------------|
| [architecture.md](docs/architecture/architecture.md) | Full system architecture with diagrams | First-time orientation |
| [auth-system.md](docs/architecture/auth-system.md) | OAuth PKCE, SA tokens, Cloud Access Policy tokens, RefreshTransport | Modifying or debugging auth |
| [cli-layer.md](docs/architecture/cli-layer.md) | Command tree, Options pattern, lifecycle | Adding/modifying CLI commands |
| [client-api-layer.md](docs/architecture/client-api-layer.md) | Dynamic client, auth, error translation | API communication changes |
| [config-system.md](docs/architecture/config-system.md) | Contexts, env vars, TLS, namespace resolution | Config or auth changes |
| [data-flows.md](docs/architecture/data-flows.md) | Push/Pull/Serve/Delete pipelines | Modifying resource sync |
| [login-system.md](docs/architecture/login-system.md) | `gcx login` orchestration, sentinel-retry, validation pipeline | Modifying or debugging login |
| [patterns.md](docs/architecture/patterns.md) | Recurring patterns catalog | Before implementing new features |
| [project-structure.md](docs/architecture/project-structure.md) | Build system, CI/CD, dependencies | Build issues, adding deps |
| [resource-model.md](docs/architecture/resource-model.md) | Resource, Selector, Filter, Discovery | Modifying resource handling |

See also: [docs/design/](docs/design/) for UX implementation guides, [docs/reference/](docs/reference/) for provider guides and CLI reference.

### How to Navigate

- **Starting a new feature**: Read `architecture.md` → `patterns.md` → relevant domain doc
- **Fixing a bug**: Jump directly to the relevant domain doc
- **Adding a CLI command**: Read `cli-layer.md` first, then `patterns.md`
- **Understanding a data flow**: Read `data-flows.md`
- **Adding config fields**: Read `config-system.md`
- **Debugging or modifying login**: Read `login-system.md`
- **Debugging or modifying auth**: Read `auth-system.md`
- **Modifying resource handling**: Read `resource-model.md`
- **API communication or errors**: Read `client-api-layer.md`
- **Build issues or dependencies**: Read `project-structure.md`

### Worked Examples

**How does a resource get pushed to Grafana?**
1. [data-flows.md](docs/architecture/data-flows.md) § "PUSH Pipeline" — numbered steps (parse selectors → resolve → read → push → summary)
2. [resource-model.md](docs/architecture/resource-model.md) — Selector/Filter concepts
3. [client-api-layer.md](docs/architecture/client-api-layer.md) — how Create/Update calls work

**Adding a new CLI flag to `push`:**
1. [cli-layer.md](docs/architecture/cli-layer.md) § "The Options Pattern"
2. Look at `push.go` as the canonical example
3. Add to opts struct → bind in `setup()` → validate in `Validate()`

**Adding support for a new resource type:**
1. [resource-model.md](docs/architecture/resource-model.md) § "Discovery System" — types are discovered at runtime, no hardcoding
2. [patterns.md](docs/architecture/patterns.md) § "Processor Pipeline" — if custom handling is needed
3. [data-flows.md](docs/architecture/data-flows.md) — where processors are applied

**Adding a new provider:**
1. [provider-guide.md](docs/reference/provider-guide.md) — step-by-step implementation guide
2. [patterns.md](docs/architecture/patterns.md) § "Provider Plugin System" — interface, registration, TypedCRUD
3. [provider-checklist.md](docs/design/provider-checklist.md) — UX compliance checklist

**Debugging an authentication issue:**
1. [config-system.md](docs/architecture/config-system.md) § "Auth Priority" — token vs user/password precedence
2. [client-api-layer.md](docs/architecture/client-api-layer.md) — how auth wires into `rest.Config`
3. [config-system.md](docs/architecture/config-system.md) — env var override behavior

## Taste Rules

Enforced — see [CONSTITUTION.md § Taste Rules](CONSTITUTION.md#taste-rules) for the authoritative list.

- **Options pattern** for every command: `opts` struct → `setup(flags)` → `Validate()` → constructor
- **Error messages**: lowercase, no trailing punctuation
- **Table-driven tests**: all Go tests follow [Go wiki conventions](https://go.dev/wiki/TableDrivenTests)
- **errgroup concurrency**: bounded parallelism (default 10) for all batch I/O operations
- **Commit format**: Title (one-liner) / What (description) / Why (rationale)

## Related

- [VISION.md](VISION.md) — goals, product surface, roadmap themes
- [CONSTITUTION.md](CONSTITUTION.md) — architecture invariants and dependency rules
- [DESIGN.md](DESIGN.md) — CLI UX design, command grammar, output model
- [docs/reference/provider-guide.md](docs/reference/provider-guide.md) — how to add a new provider
