# Architecture: gcx

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

## Detailed Architecture Docs

[docs/architecture/README.md](docs/architecture/README.md) is the full index. It covers:

| Document | Domain |
|----------|--------|
| [architecture.md](docs/architecture/architecture.md) | Full system architecture with diagrams |
| [patterns.md](docs/architecture/patterns.md) | Recurring patterns catalog |
| [resource-model.md](docs/architecture/resource-model.md) | Resource, Selector, Filter, Discovery abstractions |
| [cli-layer.md](docs/architecture/cli-layer.md) | Command tree, Options pattern, lifecycle |
| [client-api-layer.md](docs/architecture/client-api-layer.md) | Dynamic client, auth, error translation |
| [config-system.md](docs/architecture/config-system.md) | Contexts, env vars, TLS, namespace resolution |
| [data-flows.md](docs/architecture/data-flows.md) | Push/Pull/Serve/Delete pipelines |
| [project-structure.md](docs/architecture/project-structure.md) | Build system, CI/CD, dependencies |

Package map (compact) is in [AGENTS.md](AGENTS.md). Detailed package descriptions are in [docs/architecture/project-structure.md](docs/architecture/project-structure.md).

## Related

- [CONSTITUTION.md](CONSTITUTION.md) — architecture invariants and dependency rules
- [DESIGN.md](DESIGN.md) — CLI UX design, command grammar, output model
- [docs/reference/provider-guide.md](docs/reference/provider-guide.md) — how to add a new provider
