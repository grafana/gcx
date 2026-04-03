# Design: gcx

## Vision

kubectl-style CLI for managing Grafana 12+ resources via its Kubernetes-compatible API.
Built in Go (~14k LOC), uses `k8s.io/client-go` and Cobra. Enables managing dashboards,
folders, alert rules, SLOs, synthetic monitoring checks, and datasource queries from
a single tool with multi-environment context support.

## Pipeline

```
CLI Layer (cmd/gcx/)          -- Cobra commands, zero business logic
    |
    v
Business Logic (internal/resources/) -- Resource model, selectors, filters, processors
    |
    v
Client Layer (internal/resources/dynamic/) -- k8s dynamic client wrapper
    |
    v
Grafana REST API (/apis endpoint)    -- K8s-compatible API (Grafana 12+)
```

**Core flow:** User input --> Selector (partial) --> Discovery --> Filter (resolved) --> Dynamic Client --> Grafana API

### Extension Pipelines

```
Provider System (internal/providers/)     -- Pluggable Cloud product providers
    |                                        TypedRegistrations() → adapter.Register()
    v
Grafana REST API (/api endpoint)          -- Product-specific REST endpoints

Setup System (cmd/gcx/setup/)            -- Onboarding and declarative product config
    |                                        (not a provider — standalone command area)
    v
Fleet/Instrumentation APIs               -- via internal/fleet/ and internal/setup/instrumentation/

Query Layer (internal/query/)             -- Prometheus, Loki, Pyroscope, Tempo
    |                                        (direct HTTP, no k8s machinery)
    v
Datasource HTTP APIs                      -- PromQL, LogQL, profile, trace queries
```

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
| [014](docs/adrs/instrumentation/001-instrumentation-provider-design.md) | Declarative Instrumentation Setup under `gcx setup` | proposed |
| [015](docs/adrs/faro-provider/001-faro-provider-design.md) | Faro provider: CLI UX, TypedCRUD adapter, sourcemaps as sub-resource verbs | proposed |

See [docs/adrs/](docs/adrs/) for all ADRs.

## Package Map

| Package | Responsibility |
|---------|---------------|
| `cmd/gcx/root/` | CLI root (logging, global flags) |
| `cmd/gcx/config/` | Config management commands |
| `cmd/gcx/resources/` | Resource CRUD commands (get, push, pull, delete, edit, validate) |
| `cmd/gcx/dashboards/` | Dashboard snapshot command |
| `cmd/gcx/datasources/` | Datasource commands (list, get, query subcommands) |
| `cmd/gcx/providers/` | Provider list command |
| `cmd/gcx/api/` | Raw API passthrough command |
| `cmd/gcx/linter/` | Linting commands (run, new, rules, test) |
| `cmd/gcx/dev/` | Developer commands (import, scaffold, generate, lint, serve) |
| `cmd/gcx/setup/` | Setup command area (onboarding, instrumentation) |
| `cmd/gcx/setup/instrumentation/` | Instrumentation subcommands (status, discover, show, apply) |
| `cmd/gcx/fail/` | Structured error to user-friendly message conversion |
| `internal/config/` | Config types, loader, editor, rest.Config builder |
| `internal/resources/` | Core types: Resource, Selector, Filter, Descriptor |
| `internal/resources/adapter/` | ResourceAdapter interface, Factory, ResourceClientRouter, TypedCRUD[T], TypedObject[T], ResourceIdentity, ResourceNamer, SchemaFromType[T], slug-ID helpers (SlugifyName, ExtractIDFromSlug, ComposeName) |
| `internal/resources/discovery/` | API resource discovery, registry, GVK resolution |
| `internal/resources/dynamic/` | k8s dynamic client wrapper |
| `internal/resources/local/` | FSReader, FSWriter (disk I/O) |
| `internal/resources/process/` | Processors: ManagerFields, ServerFields, Namespace |
| `internal/resources/remote/` | Pusher, Puller, Deleter, FolderHierarchy, Summary |
| `internal/providers/` | Provider plugin system (interface, registry, TypedRegistrations) |
| `internal/providers/slo/` | SLO provider (definitions, reports) |
| `internal/providers/synth/` | Synthetic Monitoring provider (checks, probes) |
| `internal/providers/alert/` | Alert provider (rules, groups — read-only) |
| `internal/providers/oncall/` | OnCall provider (16 resource types: integrations, schedules, shifts, etc.) |
| `internal/providers/fleet/` | Fleet Management provider (pipelines, collectors) |
| `internal/providers/k6/` | K6 Cloud provider (projects, load tests, schedules, env vars, load zones) |
| `internal/providers/kg/` | Knowledge Graph (Asserts) provider (rules, datasets, vendors, entity-types, scopes — read-only adapters; entities — provider CLI only) |
| `internal/providers/incidents/` | IRM Incidents provider |
| `internal/providers/metrics/` | Metrics signal provider (Prometheus queries + Adaptive Metrics) |
| `internal/providers/logs/` | Logs signal provider (Loki queries + Adaptive Logs) |
| `internal/providers/traces/` | Traces signal provider (Tempo queries + Adaptive Traces) |
| `internal/providers/profiles/` | Profiles signal provider (Pyroscope queries + adaptive stub) |
| `internal/auth/adaptive/` | Shared adaptive telemetry auth (GCOM caching, Basic auth) — imported by signal adaptive subpackages |
| `internal/providers/faro/` | Faro provider (Frontend Observability — apps CRUD, sourcemaps sub-resource verbs) |
| `internal/providers/appo11y/` | App Observability provider (overrides, settings — singleton TypedCRUD resources) |
| `internal/dashboards/` | Dashboard Image Renderer client |
| `internal/query/prometheus/` | Prometheus HTTP query client |
| `internal/query/loki/` | Loki HTTP query client |
| `internal/agent/` | Agent mode detection |
| `internal/terminal/` | TTY/pipe detection |
| `internal/linter/` | Linting engine (Rego rules, PromQL/LogQL validators) |
| `internal/graph/` | Terminal chart rendering |
| `internal/server/` | Live dev server (Chi router, reverse proxy, websocket reload) |
| `internal/grafana/` | OpenAPI client (health checks, version detection) |
| `internal/output/` | Output codec registry (json, yaml, text, wide — field selection, discovery, k8s unstructured handling) |
| `internal/format/` | JSON/YAML codecs |
| `internal/fleet/` | Shared fleet base client (HTTP, auth, config — shared between fleet provider and setup/instrumentation) |
| `internal/setup/instrumentation/` | Manifest types (InstrumentationConfig), instrumentation client, optimistic lock comparison |
| `internal/cloud/` | GCOM HTTP client for Grafana Cloud stack discovery |
| `internal/httputils/` | HTTP helpers (used by serve command's proxy) |
| `internal/secrets/` | Redactor for config view |
| `internal/logs/` | slog/klog integration |
| `internal/testutils/` | Shared test utilities |

## Detailed Architecture Documentation

The `docs/architecture/` directory contains comprehensive architecture analysis:

- [docs/architecture/architecture.md](docs/architecture/architecture.md) — Full system architecture
- [docs/architecture/patterns.md](docs/architecture/patterns.md) — Recurring patterns
- [docs/architecture/resource-model.md](docs/architecture/resource-model.md) — Resource, Selector, Filter, Discovery
- [docs/architecture/cli-layer.md](docs/architecture/cli-layer.md) — Command tree, Options pattern
- [docs/architecture/client-api-layer.md](docs/architecture/client-api-layer.md) — Dynamic client, auth
- [docs/architecture/config-system.md](docs/architecture/config-system.md) — Contexts, env vars, TLS
- [docs/architecture/data-flows.md](docs/architecture/data-flows.md) — Push/Pull/Serve/Delete pipelines
- [docs/architecture/project-structure.md](docs/architecture/project-structure.md) — Build system, CI/CD
- [docs/reference/provider-guide.md](docs/reference/provider-guide.md) — How to add a new provider
- [docs/reference/design-guide.md](docs/reference/design-guide.md) — UX requirements

## Reference Documentation

- [docs/README.md](docs/README.md) — Full documentation index
- [CONSTITUTION.md](CONSTITUTION.md) — Project invariants and constraints
  - [CLI Grammar](CONSTITUTION.md#cli-grammar) — Command structure (`$AREA $NOUN $VERB`)
  - [Dual-Purpose Design](CONSTITUTION.md#dual-purpose-design) — Human/agent command design
  - [Push/Pull Philosophy](CONSTITUTION.md#pushpull-philosophy) — Local manifest workflow
  - [Provider Architecture](CONSTITUTION.md#provider-architecture) — Dual CRUD paths and adapter requirements
- [CONTRIBUTING.md](CONTRIBUTING.md) — Development setup and workflow
