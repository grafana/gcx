# Agent Documentation Index

> Generated: 2026-03-03 | Strategy: Standard | Confidence: 92%
>
> This directory contains high-level architecture documentation for autonomous coding agents.
> Start here, then navigate to specific docs as needed.

## Quick Navigation

| Document | Focus Area | When to Read |
|----------|------------|--------------|
| [architecture.md](architecture.md) | Full architecture overview | First-time orientation, understanding system design |
| [patterns.md](patterns.md) | Recurring patterns catalog | Before implementing new features, understanding conventions |
| [resource-model.md](resource-model.md) | Core abstractions (Resource, Selector, Filter, Discovery) | Modifying resource handling, adding resource types |
| [cli-layer.md](cli-layer.md) | Command patterns, Options struct, lifecycle | Adding/modifying CLI commands |
| [client-api-layer.md](client-api-layer.md) | Grafana API communication, dynamic client | Modifying API calls, auth, error handling |
| [config-system.md](config-system.md) | Configuration, contexts, env vars | Adding config fields, auth changes |
| [data-flows.md](data-flows.md) | Push/Pull/Serve pipelines | Modifying resource sync operations |
| [project-structure.md](project-structure.md) | Build system, CI/CD, dependencies | Build issues, adding dependencies, release process |
| [provider-guide.md](provider-guide.md) | Step-by-step guide for implementing a new provider | Adding a new Grafana product provider |
| [design-guide.md](design-guide.md) | UX requirements for commands and providers | Before implementing new features, reviewing CLI UX decisions |

## Architecture at a Glance

**grafanactl** is a kubectl-style CLI for managing Grafana resources via Grafana 12+'s Kubernetes-compatible API. It uses `k8s.io/client-go` and `k8s.io/apimachinery` directly because the Grafana server exposes a `/apis` endpoint with standard Kubernetes semantics.

The architecture follows a clean layered monolith with strict separation: CLI wiring (`cmd/`) holds no business logic; all domain logic lives in `internal/` organized by feature (config, resources, server). Resources are represented as `unstructured.Unstructured` objects (map-based, no pre-generated Go types), which enables dynamic discovery of resource types and leverages the full Kubernetes client ecosystem including pagination, dry-run semantics, and error handling patterns.

A composable processor pipeline transforms resources during push and pull operations, keeping I/O and transformation concerns decoupled. Context-based multi-environment configuration follows the kubectl kubeconfig pattern, enabling management of multiple Grafana instances from a single config file with named contexts.

Two extension subsystems complement the core resource management path: the **provider plugin system** (`internal/providers/`) which registers Prometheus/Loki datasource configs with secret redaction, and the **datasource query layer** (`internal/query/`) which provides direct HTTP clients for PromQL/LogQL queries with terminal graph rendering (`internal/graph/`).

## Key Patterns Quick Reference

- **Kubernetes Resource Model Adoption (97% confidence)**: Direct use of `k8s.io/apimachinery` and `k8s.io/client-go` because Grafana 12+ exposes a `/apis` endpoint with K8s semantics. All resources are unstructured objects with discovery at runtime.

- **Options Pattern for CLI Commands (96% confidence)**: Every `resources` subcommand follows a four-part structure: opts struct → setup(flags) → Validate() → constructor that wires opts into cobra.Command. Shared concerns (OnErrorMode, io.Options, configOpts) are composed via embedding.

- **Processor Pipeline (94% confidence)**: Resource transformations modeled as a `Processor` interface with a single `Process(res *Resource) error` method. Processors compose into ordered slices applied at well-defined points in push/pull pipelines.

- **Selector-to-Filter Resolution (95% confidence)**: User input flows through two-stage resolution: CLI argument → Selector (partial, unvalidated) → Discovery Registry → Filter (fully resolved, complete GVK). Keeps CLI layer ignorant of API details.

- **Dual-Client Architecture (93% confidence)**: Dynamic client path uses `/apis` (K8s-compatible) with `k8s.io/client-go` for resource CRUD; OpenAPI client uses `/api` (Grafana REST) for health checks and version discovery.

- **Provider Plugin System**: Interface + registry pattern for datasource providers (Prometheus, Loki). Each provider exposes typed config, a config key for lookup, and integrates with the secret redactor for safe display in `config view`.

- **Direct HTTP Client for Datasource APIs**: Query clients (`internal/query/prometheus`, `internal/query/loki`) bypass the k8s dynamic client and call datasource HTTP APIs directly, enabling PromQL/LogQL execution with results rendered as terminal charts via `internal/graph/`.

## How to Use These Docs

- **Starting a new feature**: Read `architecture.md` → `patterns.md` → relevant domain doc
- **Fixing a bug**: Jump directly to the relevant domain doc
- **Adding a CLI command**: Read `cli-layer.md` first, then reference `patterns.md`
- **Understanding a data flow**: Read `data-flows.md`
- **Adding config fields or auth methods**: Read `config-system.md`
- **Modifying resource handling**: Read `resource-model.md`
- **API communication or error handling**: Read `client-api-layer.md`
- **Build issues or dependencies**: Read `project-structure.md`

## Document Descriptions

### [architecture.md](architecture.md)
Full system architecture with layered structure, core abstractions, and design decisions. Contains high-level diagrams and comprehensive coverage of all major components. Start here for first-time orientation.

### [patterns.md](patterns.md)
Recurring patterns catalog with Kubernetes resource model adoption, options pattern for CLI, processor pipeline, selector resolution, dual-client architecture, and more. Read before implementing new features.

### [resource-model.md](resource-model.md)
Core abstractions deep-dive: Resource type wrapping unstructured objects, Selector parsing, Filter resolution, Discovery registry, and the selector→filter→resource pipeline. Essential for modifying resource handling.

### [cli-layer.md](cli-layer.md)
Command tree structure, file layout of cmd/grafanactl/, the Options pattern used by all resource commands, error handling, and output formatting. Read when adding or modifying CLI commands.

### [client-api-layer.md](client-api-layer.md)
Client construction chain from GrafanaConfig through NamespacedRESTConfig to dynamic/versioned clients. Covers both the Kubernetes-compatible `/apis` path (primary) and Grafana OpenAPI `/api` path (secondary). Essential for API communication changes.

### [config-system.md](config-system.md)
Context-based configuration model inspired by kubectl kubeconfig. Data model, loading chain, environment variable overrides, namespace resolution (org-id vs stack-id), and TLS configuration. Read when modifying configuration or auth.

### [data-flows.md](data-flows.md)
Four primary pipelines: PUSH (local→Grafana), PULL (Grafana→local), DELETE (local→Grafana), SERVE (local→preview→browser). Detailed flow diagrams with concurrency models, processor application points, and error handling.

### [project-structure.md](project-structure.md)
Directory layout rationale, build system (Makefile), CI/CD (GitHub Actions via .goreleaser.yaml), test fixtures, and dependency management. Contains notes on vendoring, devbox toolchain, and documentation generation.

---

## Confidence Scores

| Section | Score | Notes |
|---------|-------|-------|
| Project Structure | 96% | Exhaustive analysis of directory layout, build system, CI/CD |
| Resource Model | 95% | Core abstractions thoroughly documented with type relationships |
| CLI Layer | 94% | Complete command tree and options pattern analysis |
| Client/API Layer | 93% | Both client paths documented; minor gaps in retry/timeout |
| Config System | 95% | Full loading chain and environment overrides covered |
| Data Flows | 94% | All four pipelines documented with concurrency models |
| **Overall** | **92%** | High-quality analysis across all domains |

---

## Quick Start Examples

### I need to understand how a resource gets pushed to Grafana
1. Read the "User invocation" section in [data-flows.md](data-flows.md) under "2. PUSH Pipeline"
2. Follow the numbered pipeline steps (1-5: parse selectors → resolve → read → push → update config)
3. Reference [resource-model.md](resource-model.md) for Selector/Filter concepts
4. Reference [client-api-layer.md](client-api-layer.md) for how Create/Update calls work

### I'm adding a new CLI flag to the `push` command
1. Read [cli-layer.md](cli-layer.md) section "The Options Pattern"
2. Look at push.go as the canonical example
3. Understand the four-part structure: opts struct → setup() → Validate() → constructor
4. Add your flag to the opts struct, bind it in setup(), validate in Validate()

### I need to add support for a new Grafana resource type
1. Check [resource-model.md](resource-model.md) section "Discovery System"
2. Understand that resource types are discovered at runtime — no hardcoding needed
3. If you need custom handling, reference the "Processor Pipeline" in [patterns.md](patterns.md)
4. Check [data-flows.md](data-flows.md) for where processors are applied

### I'm debugging an authentication issue
1. Read [config-system.md](config-system.md) section "Auth Priority"
2. Understand the token vs user/password precedence rules
3. Reference [client-api-layer.md](client-api-layer.md) for how auth is wired into rest.Config
4. Check environment variable override behavior in [config-system.md](config-system.md)

---

*Last updated: 2026-03-04 | Validation: All files present and verified*
