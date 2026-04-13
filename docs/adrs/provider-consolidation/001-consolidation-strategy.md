# ADR-001: Provider Consolidation Strategy

**Created**: 2026-03-20
**Status**: accepted
**Bead**: none
**Supersedes**: none

## Context

Two Grafana CLIs exist with complementary strengths:

- **Cloud CLI** — broad product coverage (OnCall, k6, Fleet, Incidents, Knowledge Graph, ML, SCIM, etc.) and a polished agentic UX (agent annotations, `commands` JSON tree, agent-card, audit logging). Architecture is provider-per-command with hand-written adapters.
- **gcx** — solid architecture (K8s-compatible API tier, pluggable `Provider` + `ResourceAdapter` pattern, `TypedCRUD[T]` generic, push/pull/diff pipelines) but narrower product coverage (K8s-native resources only: dashboards, folders, datasources, alert rules, SLOs, synth).

The team needs a **single CLI for GrafanaCon 2026** — one binary that covers all Grafana Cloud products with gcx's architectural discipline.

The core tension: the cloud CLI has the product breadth, gcx has the right architecture. Merging them requires choosing a base and porting the other's capabilities.

## Decision

**gcx is the consolidation base.** Cloud CLI providers are ported into gcx using the existing `Provider` + `ResourceAdapter` + `TypedCRUD[T]` pattern.

### Why gcx as base

1. **Architecture scales better.** The K8s-tier + provider-tier separation is clean. K8s-native resources get automatic CRUD with zero adapter code as Grafana migrates to app platform. The cloud CLI's per-command architecture would require rearchitecting everything.
2. **TypedCRUD[T] eliminates porting boilerplate.** The generic handles JSON↔unstructured conversion, K8s envelope wrapping, and name/ID management. Porting a cloud CLI resource client is ~30 LOC of wiring vs ~200 LOC of hand-written adapter code.
3. **Push/pull/delete pipelines are already built.** gcx's resource sync infrastructure (processors, local FS reader/writer, remote pusher/puller) works for all providers without modification.
4. **gcx has better test infrastructure and linting.** Migrating to the cloud CLI's base would mean rebuilding these.

### TypedCRUD[T] as the porting vehicle

Every cloud CLI resource client is ported as:

```
internal/providers/{provider}/
├── provider.go          # Provider impl + init() registration
├── types.go             # API structs (ported from the cloud CLI)
├── client.go            # HTTP client (adapted to gcx's config)
└── resource_adapter.go  # TypedCRUD[T] wiring (~30 LOC)
```

The `TypedCRUD[T]` generic reduces per-resource adapter code from ~200 LOC to ~30 LOC:

```go
// Each resource registration is ~10 LOC
adapter.Register(adapter.Registration{
    Factory: func(ctx context.Context) (adapter.ResourceAdapter, error) {
        cfg, err := loader.LoadCloudConfig(ctx)
        client := NewClient(cfg)
        return &adapter.TypedCRUD[Schedule]{
            NameFn: func(s Schedule) string { return s.ID },
            ListFn: func(ctx context.Context) ([]Schedule, error) {
                return client.ListSchedules(ctx)
            },
            GetFn: func(ctx context.Context, name string) (*Schedule, error) {
                return client.GetSchedule(ctx, name)
            },
        }.AsAdapter(), nil
    },
    Descriptor: scheduleMeta.Descriptor,
    GVK:        scheduleMeta.Descriptor.GroupVersionKind(),
})
```

### Phased approach

Work proceeds in dependency order, not by product:

```
Phase 0: Foundation — TypedCRUD[T] generic (already built)
    ↓
Phase 1: Complex Providers (OnCall, k6, Fleet, Incidents, KG, ML, SCIM, GCom)
    ↓ (parallelizable)
Phase 2: UX/AX (agent annotations, commands tree, audit logging, config enhancements)
    ↓
Phase 3: Non-K8s Grafana REST resources (Annotations, Library Panels, Teams, etc.)
    ↓
Phase 4: Existing resource extras (Dashboard versions, Datasource correlations)
    ↓
Phase 5: Init/Onboarding (gcx init, permissions registry)
```

Phase 1 and 2 are parallelizable. GrafanaCon critical path: Phase 0 (done) + Phase 1 top providers (3-4w) + Phase 2 key items (2w).

### What is not in scope

- Absorbing assistant-cli (separate effort, depends on IAM team)
- OAuth onboarding flow (depends on assistant-cli absorption)
- OTEL self-observability (dropped — binary bloat)
- MCP server (dropped — shell-mode sufficient for current agents)
- `get X` verb shortcuts (dropped — `resources get X` is fine)

## Consequences

### Positive
- Single CLI covering all Grafana Cloud products
- The cloud CLI's product breadth gains gcx's architectural discipline (K8s tier, TypedCRUD, pipelines)
- TypedCRUD[T] reduces porting effort — ~30 LOC per resource vs ~200 LOC hand-written
- K8s-native resources will "just work" as Grafana migrates products to app platform (no re-porting needed)
- gcx's push/pull/delete pipelines apply to all provider resources without modification

### Negative
- The cloud CLI's agentic UX features (agent annotations, `commands` tree, audit logging) must be ported into gcx — they don't come for free
- Cloud CLI users must migrate to gcx; the cloud CLI will be deprecated
- Complex providers (OnCall with 17 resource types, k6 with multi-tenant auth) require bespoke TypedCRUD wiring patterns

### Neutral
- Cloud CLI resource clients are ported as-is; API types and HTTP clients are preserved
- Existing gcx K8s-native resources are unaffected
- Already-built features (datasources MLTP, telemetry viz, o11y-as-code) require no migration work
