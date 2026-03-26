---
type: feature-plan
title: "ConfigLoader Compliance: Unified Config Loading Across All Providers"
status: draft
spec: docs/specs/feature-configloader-compliance/spec.md
created: 2026-03-26
---

# Architecture and Design Decisions

## Pipeline Architecture

### Current State

```
┌─────────────────────────────────────────────────────────────────┐
│                     Config Loading Today                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  providers.ConfigLoader (shared)                                 │
│  ├── LoadGrafanaConfig()    ← used by: slo, alert, fleet,      │
│  │                            incidents, kg, oncall              │
│  ├── LoadCloudConfig()      ← used by: k6, fleet               │
│  └── BindFlags()                                                 │
│                                                                  │
│  synth/configLoader (DUPLICATE ~100 lines)                       │
│  ├── loadConfig()           ← reimplements env override logic   │
│  ├── envOverride()          ← duplicates providers.envOverride  │
│  ├── configSource()         ← duplicates file resolution        │
│  ├── LoadSMConfig()         ← legacy GRAFANA_SM_* env vars     │
│  ├── LoadGrafanaConfig()    ← reimplements providers version    │
│  ├── LoadConfig()           ← full config access                │
│  ├── SaveMetricsDatasourceUID() ← config write-back             │
│  └── bindFlags()                                                 │
│                                                                  │
│  oncall/configLoader (embeds providers.ConfigLoader)             │
│  ├── LoadOnCallClient()     ← uses LoadGrafanaConfig            │
│  └── discoverOnCallURL()    ← ad-hoc os.Getenv for legacy var  │
│                                                                  │
│  k6 (uses providers.ConfigLoader directly)                       │
│  └── authenticatedClient()  ← ad-hoc cfg.ProviderConfig("k6")  │
│                                                                  │
│  alert, fleet, incidents, kg, slo                                │
│  └── Use providers.ConfigLoader directly (no custom logic)       │
└─────────────────────────────────────────────────────────────────┘
```

### Target State

```
┌─────────────────────────────────────────────────────────────────┐
│              ConfigLoader with New Methods                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  providers.ConfigLoader (extended)                               │
│  ├── LoadGrafanaConfig()         ← unchanged                    │
│  ├── LoadCloudConfig()           ← unchanged                    │
│  ├── BindFlags()                 ← unchanged                    │
│  ├── LoadProviderConfig(ctx,n)   ← NEW (FR-001)                │
│  │   └── resolves GRAFANA_PROVIDER_<N>_<KEY> + config file     │
│  ├── SaveProviderConfig(ctx,n,k,v)  ← NEW (FR-002)            │
│  └── LoadFullConfig(ctx)         ← NEW (FR-003)                │
│                                                                  │
│  ┌───────────────────────────────────────────────────┐          │
│  │ synth (FR-004..FR-008)                            │          │
│  │  providers.ConfigLoader (embedded)                │          │
│  │  LoadSMConfig → calls LoadProviderConfig("synth") │          │
│  │  SaveMetricsDatasourceUID → SaveProviderConfig    │          │
│  │  LoadConfig → LoadFullConfig                      │          │
│  │  LoadGrafanaConfig → delegates to embedded        │          │
│  │  ✗ local loadConfig(), envOverride(), configSource│          │
│  │  ✗ GRAFANA_SM_URL/GRAFANA_SM_TOKEN (removed)     │          │
│  └───────────────────────────────────────────────────┘          │
│                                                                  │
│  ┌───────────────────────────────────────────────────┐          │
│  │ oncall (FR-009, FR-010)                           │          │
│  │  providers.ConfigLoader (embedded, as today)      │          │
│  │  discoverOnCallURL → LoadProviderConfig("oncall") │          │
│  │  ✗ os.Getenv("GRAFANA_ONCALL_URL") (removed)     │          │
│  └───────────────────────────────────────────────────┘          │
│                                                                  │
│  ┌───────────────────────────────────────────────────┐          │
│  │ k6 (FR-011)                                       │          │
│  │  providers.ConfigLoader (as today)                │          │
│  │  authenticatedClient → uses LoadProviderConfig    │          │
│  └───────────────────────────────────────────────────┘          │
│                                                                  │
│  alert, fleet, incidents, kg, slo ← UNCHANGED (FR-012)         │
└─────────────────────────────────────────────────────────────────┘
```

### LoadProviderConfig Flow

```
LoadProviderConfig(ctx, "synth")
    │
    ├── 1. Load config via config.LoadLayered (existing logic)
    │       └── resolves GRAFANA_PROVIDER_SYNTH_* env vars
    │
    ├── 2. Extract curCtx.Providers["synth"] → map[string]string
    │
    └── 3. Return (providerCfg, namespace, nil)
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| No hooks interface — just three new methods on ConfigLoader | No current provider needs post-load hooks; YAGNI. Add hooks when a concrete use case arises. |
| All providers use GRAFANA_PROVIDER_<NAME>_<KEY> env var convention | One env var pattern to document and test. Legacy GRAFANA_SM_URL and GRAFANA_ONCALL_URL are dropped. |
| LoadProviderConfig returns (map[string]string, string, error) | Provider config map plus namespace. Namespace is universally needed. Traces to FR-001. |
| SaveProviderConfig writes a single key to a named provider section | Generalized from synth's SaveMetricsDatasourceUID. Traces to FR-002. |
| LoadFullConfig returns *config.Config | Required by synth for datasource UID lookup. Traces to FR-003. |
| synth configLoader becomes embedding of providers.ConfigLoader | Eliminates ~100 lines of duplication while maintaining smcfg interface contracts. Traces to FR-004, FR-008. |
| synth MUST use config.LoadLayered, not config.Load | ConfigLoader already uses LoadLayered; synth's config.Load is non-standard. Traces to FR-007. |
| k6 uses LoadProviderConfig directly | K6 has no special logic. Traces to FR-011. |

## Compatibility

**Unchanged (backward compatible):**
- `GRAFANA_PROVIDER_*` env var convention: unchanged
- Config file schema: no new keys, no changed keys
- Provider interface: no new methods, no changed signatures
- alert, fleet, incidents, kg, slo providers: zero code changes
- smcfg.Loader, smcfg.StatusLoader interfaces: method signatures unchanged
- oncall plugin API discovery (DiscoverOnCallURL): untouched

**Breaking changes (intentional):**
- `GRAFANA_SM_URL` env var removed — use `GRAFANA_PROVIDER_SYNTH_SM_URL`
- `GRAFANA_SM_TOKEN` env var removed — use `GRAFANA_PROVIDER_SYNTH_SM_TOKEN`
- `GRAFANA_ONCALL_URL` env var removed — use `GRAFANA_PROVIDER_ONCALL_ONCALL_URL`

**Newly available:**
- LoadProviderConfig, SaveProviderConfig, LoadFullConfig on ConfigLoader

**Removed (dead code):**
- synth/provider.go: local configLoader struct, loadConfig(), envOverride(), configSource(), bindFlags()
