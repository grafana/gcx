# App O11y Provider: CLI UX and Resource Adapter Design

**Created**: 2026-03-30
**Status**: proposed
**Bead**: gcx-4r1g (parent epic: gcx-3c3403a9)
**Supersedes**: none

## Context

grafana-cloud-cli has an `app-o11y` command group that manages the Grafana
App Observability plugin. It exposes two singleton configs via plugin proxy
REST endpoints:

- **Overrides** (`MetricsGeneratorConfig`) — metrics generator settings,
  service graph dimensions, span metrics config. Uses ETag for optimistic
  concurrency.
- **Settings** (`PluginSettings`) — plugin-level config: log query mode,
  metrics mode, query templates.

Both hit `/api/plugin-proxy/grafana-app-observability-app/...` endpoints on the
Grafana host using the standard SA token. There is no separate auth.

Key design tensions:

1. **Singleton resources vs TypedCRUD.** These are singleton configs (one per
   stack), not named resources. TypedCRUD expects `List()`/`Get(name)` semantics.
   The naive approach would be provider-only commands with alternative verbs,
   but that sacrifices the `resources` pipeline and schema/example integration.

2. **ETag handling.** Overrides uses `If-Match` for conditional writes. This
   state must survive the get→edit→update workflow, including through K8s
   envelope round-trips.

3. **Verb naming.** CONSTITUTION.md prohibits adapter verbs on provider-only
   resources, but permits them when resources are properly registered as
   adapters.

## Decision

### Singleton adapters with well-known name

Both resources are registered as TypedCRUD adapters with a hardcoded name
`"default"`. Only `GetFn` and `UpdateFn` are populated; `ListFn`, `CreateFn`,
and `DeleteFn` are nil.

This gives dual access paths:

- Provider: `gcx appo11y overrides get` / `gcx appo11y overrides update -f`
- Generic: `gcx resources get overrides.v1alpha1.appo11y.ext.grafana.app/default`

Schema and example are served by `resources schemas` / `resources examples`
automatically via adapter registration. No provider-level schema/example
commands.

**Rejected:** Provider-only commands with `show`/`apply` verbs. Would sacrifice
the `resources` pipeline, require provider-level schema/example commands, and
prevent push/pull workflows.

### ETag as K8s annotation

The ETag from the Overrides API response is stored as a K8s annotation
(`appo11y.ext.grafana.app/etag`) in the resource metadata. This survives
envelope round-trips (pull→edit→push) naturally, analogous to how
`resourceVersion` works in K8s.

Settings has no ETag — unconditional write.

**Rejected:** Hidden field on the domain type. Would require custom
marshal/unmarshal hooks and wouldn't survive file-based workflows.

### Command naming: `appo11y`

The provider name is `appo11y` (no hyphen), consistent with existing provider
directory names (`oncall`, `synth`, `fleet`, `kg`). The CLI command is
`gcx appo11y`.

**Rejected:** `app-o11y` (hyphenated). No existing provider uses hyphens in
directory names or CLI commands.

### Standard adapter verbs

Since both resources are properly registered as adapters, provider commands
use standard verbs (`get`, `update`) per CONSTITUTION.md. Alternative verbs
(`show`, `apply`) are not needed.

### Per-kind subpackages

Following the SLO provider pattern (`slo/definitions/`, `slo/reports/`), each
resource type gets its own subpackage:

```
internal/providers/appo11y/
├── provider.go              # init(), Commands(), TypedRegistrations()
├── client.go                # Shared HTTP client
├── overrides/
│   ├── types.go             # MetricsGeneratorConfig + ResourceIdentity
│   ├── adapter.go           # Descriptor, Schema, Example, ToResource/FromResource
│   ├── resource_adapter.go  # NewTypedCRUD(), LazyFactory
│   └── commands.go          # get, update, table/wide codecs
└── settings/
    ├── types.go             # PluginSettings + ResourceIdentity
    ├── adapter.go           # Descriptor, Schema, Example, ToResource/FromResource
    ├── resource_adapter.go  # NewTypedCRUD(), LazyFactory
    └── commands.go          # get, update, table/wide codecs
```

The HTTP client is shared at the provider root — same host, same auth,
different paths.

## GVK Mapping

| Kind | Group | Version | Singular | Plural |
|------|-------|---------|----------|--------|
| Overrides | `appo11y.ext.grafana.app` | `v1alpha1` | `overrides` | `overrides` |
| Settings | `appo11y.ext.grafana.app` | `v1alpha1` | `settings` | `settings` |

Singular equals plural because these are singleton resources.

## Command Tree

### Provider commands

```
gcx appo11y
├── overrides
│   ├── get                    # Show current overrides config
│   └── update -f <file>      # Update from JSON/YAML file (with ETag)
└── settings
    ├── get                    # Show current plugin settings
    └── update -f <file>      # Update from JSON/YAML file
```

### Generic resource path

```
gcx resources get overrides.v1alpha1.appo11y.ext.grafana.app/default
gcx resources get settings.v1alpha1.appo11y.ext.grafana.app/default
gcx resources schemas    # includes Overrides + Settings
gcx resources examples   # includes Overrides + Settings
```

## Output Formats

All `get` commands support four output formats via `-o` flag.

### Overrides

**table** (default):
```
NAME      COLLECTION   INTERVAL   SERVICE GRAPHS   SPAN METRICS
default   enabled      15s        enabled          enabled
```

**wide**:
```
NAME      COLLECTION   INTERVAL   SERVICE GRAPHS   SG DIMENSIONS          SPAN METRICS   SM DIMENSIONS
default   enabled      15s        enabled          http.method,http.s…    enabled        service.name,span.kind
```

**json/yaml** — K8s envelope with spec containing full config.

### Settings

**table** (default):
```
NAME      LOG QUERY MODE   METRICS MODE
default   otlp             tempoMetricsGen
```

**wide**:
```
NAME      LOG QUERY MODE   METRICS MODE      LOGS QUERY (NS)                              LOGS QUERY (NO NS)
default   otlp             tempoMetricsGen   {k8s_namespace_name="$namespace"} | json      {} | json
```

**json/yaml** — K8s envelope with spec containing full settings.

## Auth & Config

- **Auth**: Standard Grafana SA token via `rest.HTTPClientFor()`. No
  provider-specific credentials.
- **ConfigKeys()**: Returns nil (reuses `GRAFANA_TOKEN`).
- **Validate()**: Returns nil (no provider-specific config to validate).
- **Environment variables**: Standard `GRAFANA_SERVER`, `GRAFANA_TOKEN`,
  `GRAFANA_NAMESPACE` only.

## API Endpoints

| Operation | Method | Path |
|-----------|--------|------|
| Get overrides | GET | `/api/plugin-proxy/grafana-app-observability-app/overrides` |
| Set overrides | POST | `/api/plugin-proxy/grafana-app-observability-app/overrides` |
| Get settings | GET | `/api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings` |
| Set settings | POST | `/api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings` |

Response deserialization:
- Get overrides: `json.Unmarshal(body, &MetricsGeneratorConfig)` — direct struct, no wrapper
- Get settings: `json.Unmarshal(body, &PluginSettings)` — direct struct, no wrapper
- Set overrides: Status check only (discard body). Sends `If-Match: <etag>` header.
- Set settings: Status check only (discard body). No ETag.

## Consequences

- Singleton adapter pattern is new to gcx. If more singletons emerge (e.g.
  org settings, license), this establishes the `"default"` name convention.
- No `list` support means `resources get` without a name will error. The error
  message should point to `/default`.
- ETag-as-annotation is a novel pattern. If it works well, it could be
  generalized to other resources with optimistic concurrency.
