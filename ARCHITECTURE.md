# Architecture: gcx

## Vision

See [VISION.md](VISION.md) for goals, roadmap, and product surface.

**In brief:** A CLI for managing Grafana and Grafana Cloud. Supports dynamic Grafana API resources via a kubectl-like resources layer, and per-product features via the provider interface. Includes observability-as-code workflows (`gcx dev`), multi-stack configuration/contexts, and Grafana Assistant integration. Optimized for AI agents and human use.

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

## Architecture Docs

Deep-dive docs live in [docs/architecture/](docs/architecture/). Each covers one domain:

| Document | Domain | When to Read |
|----------|--------|--------------|
| [architecture.md](docs/architecture/architecture.md) | Full system architecture with diagrams | First-time orientation |
| [patterns.md](docs/architecture/patterns.md) | Recurring patterns catalog | Before implementing new features |
| [resource-model.md](docs/architecture/resource-model.md) | Resource, Selector, Filter, Discovery | Modifying resource handling |
| [cli-layer.md](docs/architecture/cli-layer.md) | Command tree, Options pattern, lifecycle | Adding/modifying CLI commands |
| [client-api-layer.md](docs/architecture/client-api-layer.md) | Dynamic client, auth, error translation | API communication changes |
| [config-system.md](docs/architecture/config-system.md) | Contexts, env vars, TLS, namespace resolution | Config or auth changes |
| [data-flows.md](docs/architecture/data-flows.md) | Push/Pull/Serve/Delete pipelines | Modifying resource sync |
| [project-structure.md](docs/architecture/project-structure.md) | Build system, CI/CD, dependencies | Build issues, adding deps |

See also: [docs/design/](docs/design/) for UX implementation guides, [docs/reference/](docs/reference/) for provider guides and CLI reference.

### How to Navigate

- **Starting a new feature**: Read `architecture.md` → `patterns.md` → relevant domain doc
- **Fixing a bug**: Jump directly to the relevant domain doc
- **Adding a CLI command**: Read `cli-layer.md` first, then `patterns.md`
- **Understanding a data flow**: Read `data-flows.md`
- **Adding config fields or auth**: Read `config-system.md`
- **Modifying resource handling**: Read `resource-model.md`
- **API communication or errors**: Read `client-api-layer.md`
- **Build issues or dependencies**: Read `project-structure.md`

### Key Patterns

- **K8s Resource Model**: Direct use of `k8s.io/apimachinery` and `k8s.io/client-go`. All resources are `unstructured.Unstructured` with discovery at runtime — no pre-generated Go types.
- **Options Pattern**: Every command follows opts struct → `setup(flags)` → `Validate()` → constructor. Shared concerns composed via embedding.
- **Processor Pipeline**: `Processor.Process(*Resource) error` — composable transformations applied at defined points in push/pull pipelines.
- **Selector → Filter Resolution**: CLI argument → Selector (partial) → Discovery Registry → Filter (fully resolved GVK). Keeps CLI layer ignorant of API details.
- **Dual-Client Architecture**: `/apis` path (K8s-compatible, `k8s.io/client-go`) for resource CRUD; `/api` path (Grafana REST) for health checks and version discovery.
- **Provider Plugin System**: Interface + registry. Each provider self-registers via `init()` and contributes CLI commands + resource adapters.
- **Direct HTTP for Datasources**: Query clients bypass the k8s dynamic client, call datasource HTTP APIs directly (PromQL, LogQL, Pyroscope, Tempo).

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
