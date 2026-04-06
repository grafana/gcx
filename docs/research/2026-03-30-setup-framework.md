# Research: `gcx setup` — Unified Onboarding Framework

**Date**: 2026-03-30
**Status**: exploratory
**Bead**: gcx-fc9fe431.1
**Related ADR**: docs/adrs/instrumentation/001-instrumentation-provider-design.md

## Summary

This document captures the design exploration for a generalized `gcx setup`
onboarding framework. Instrumentation is the first product to ship under
this framework (see related ADR). This research covers the broader vision
for future products and the Phase 2 formal interface.

## Problem

Grafana Cloud onboarding is fragmented across products. Each product invents
its own setup/enable/init commands with no shared structure:

| Product | grafana-cloud-cli | gcx (current) |
|---------|------------------|---------------|
| Auth bootstrap | `gcx init` | Manual `config set` |
| KG | `kg setup` + `kg enable` | `kg setup` + `kg enable` |
| Instrumentation | `fleet discovery setup` + `fleet instrumentation app set` | Nothing |
| Integrations | `integrations install <slug>` | Nothing |
| Plugins | `plugins install` / `plugins incidents enable` | Nothing |

Yet all follow the same pattern: **prerequisites → discover → configure → verify**.

## Vision

```
gcx setup
├── status                              Aggregated product matrix
├── pull -d ./dir/                      Export all product configs
├── apply -f ./dir/ | file              Apply configs (directory or file)
│
├── instrumentation                     Per-product subtree (Stage 1)
│   ├── status [--cluster <name>]
│   ├── discover --cluster <name>
│   ├── show <cluster> [-o yaml]
│   └── apply -f file [--dry-run]
│
├── kg                                  Future product
│   ├── status
│   └── apply
│
├── integrations                        Future product
│   ├── status [--installed]
│   ├── discover
│   └── apply <slug>
│
└── ...                                 More products plug in
```

Grammar: `$AREA $NOUN $VERB` — `setup` is the area, product name is the
noun, operation is the verb.

## Status Command

`gcx setup status` aggregates across all registered products:

```
PRODUCT            ENABLED   HEALTH    DETAILS
instrumentation    ✓         healthy   2 clusters, 5 namespaces instrumented
kg                 ✓         healthy   1,247 entities
integrations       ✓         -         3 installed (linux-node, docker, nginx)
slo                ✗         -         -
```

Each product reports: enabled (bool), health (healthy/degraded/unknown),
and a summary string.

## Manifest Format

Per-product manifests under `setup.grafana.app/v1alpha1`. One file per
logical unit (e.g., one per cluster for instrumentation, one for KG, one
for integrations). Pull writes individual files into a directory; apply
accepts a file or directory.

```
$ gcx setup pull -d ./my-stack/
  → my-stack/
     ├── instrumentation/
     │   ├── prod-1.yaml
     │   └── staging-1.yaml
     ├── kg.yaml
     └── integrations.yaml

$ gcx setup apply -f ./my-stack/             # Apply everything
$ gcx setup instrumentation apply -f prod-1.yaml  # Apply one product
```

Manifests are environment-agnostic — product-specific URLs and credentials
are auto-populated from the target stack context.

## Phase 2: Formal Interface

```go
type SetupProduct interface {
    Name() string
    Status(ctx context.Context) (*ProductStatus, error)
    Discover(ctx context.Context, opts DiscoverOpts) ([]DiscoveredResource, error)
    Show(ctx context.Context, id string) ([]byte, error)     // Returns manifest YAML
    Apply(ctx context.Context, manifest []byte, dryRun bool) (*ApplyResult, error)
}
```

Products implement this interface and register via `setup.Register()`.
The framework handles:
- Status aggregation across products
- Directory-based pull/apply routing manifests to the right product by Kind
- Structured JSON output in agent mode

## Aspirational: Resource Adapter Integration

If setup manifests implement `ResourceIdentity` and register via
`TypedRegistrations()`, they could work with `gcx resources push/pull`:

```bash
gcx resources pull instrumentationconfigs.setup.grafana.app
gcx resources push -f setup/prod-1.yaml
```

The main question is whether the instrumentation API's upsert semantics
fit the CRUD adapter model. Evaluate after Stage 1 ships.

## Onboarding Categories Identified

From analysis of grafana-cloud-cli:

```
1. AUTH BOOTSTRAP     gcx init → "I can talk to Grafana Cloud"
2. PRODUCT ENABLE     kg setup / plugins install → "Product X is turned on"
3. AGENT DEPLOY       helm install alloy → "Collector is running in my cluster"
4. SIGNAL CONFIG      instrumentation set → "I'm collecting signals"
5. VERIFY             status / doctor → "It's all working"
```

Not all categories need `gcx setup` commands. Agent deployment (category 3)
is a K8s operation outside gcx scope. Auth bootstrap (category 1) may live
under `gcx config init` rather than `gcx setup`. The framework primarily
targets categories 2 and 4.

## Imperative vs Declarative

The framework is declarative-first: manifests are the primary artifact.
Stage 2 adds imperative convenience verbs (`add`) for quick edits by
agents and humans. The rule: imperative commands are sugar that produce
the same state capturable by `show -o yaml`.

See the instrumentation ADR for the detailed rationale on this choice.

## Open Questions

- Exact `SetupProduct` interface shape (refine during implementation)
- Whether `gcx setup` should also host auth bootstrap (`init`)
- Resource adapter integration feasibility
- How to handle products with no meaningful "discover" step (e.g., KG)
- Manifest schema versioning strategy
