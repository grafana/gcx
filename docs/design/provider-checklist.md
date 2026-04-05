# Provider Command Checklist

> UX compliance checklist for new providers, plus Provider/Resources output consistency rules, the TypedCRUD pattern, and ConfigLoader usage.
> Status markers: **[CURRENT]** = enforced, **[ADOPT]** = new code must follow, **[PLANNED]** = future.

Extends the interface checklist in [provider-guide.md](../reference/provider-guide.md) with
UX requirements. All items are `[ADOPT]` unless marked otherwise.

---

## 7. Provider Command Checklist

### Interface Compliance `[CURRENT]`

- [ ] Struct implements all five `Provider` interface methods
- [ ] `Name()` is lowercase, unique, and stable (it's the config map key)
- [ ] All config keys are declared in `ConfigKeys()`
- [ ] Secret keys (passwords, tokens, API keys) have `Secret: true`
- [ ] `Validate()` returns error pointing to `gcx config set ...`
- [ ] Provider added to `internal/providers/registry.go:All()`

### UX Compliance `[ADOPT]`

- [ ] All data-display commands support `-o json/yaml` (inherited from `io.Options`)
- [ ] List/get commands register a `text` table codec as default format
- [ ] List/get commands register a `wide` codec showing additional detail columns
- [ ] Error messages include actionable suggestions with exact CLI commands
- [ ] No `os.Exit()` calls in command code — return errors, let `handleError` exit
- [ ] Status messages use `cmdio.Success/Warning/Error/Info`
- [ ] `--config` and `--context` inherited via `configOpts` persistent flags
- [ ] Destructive operations document `--dry-run` support
- [ ] Help text follows [help-text.md](help-text.md) standards (Short/Long/Examples)
- [ ] Push-like operations are idempotent (create-or-update)
- [ ] Data fetching is format-agnostic — do not gate fetches on `--output` value (Pattern 13)
- [ ] PromQL queries use `promql-builder` (`github.com/grafana/promql-builder/go/promql`), not string formatting (Pattern 14)
- [ ] List/get commands for CRUD resources wrap json/yaml output in K8s envelope manifests (see below)
- [ ] Table output shows `NAME` (the slug-id or user-facing identifier), not bare numeric `ID` — users need the NAME for get/update/delete commands (see Slug-ID naming below)

### Slug-ID Naming in Tables `[ADOPT]`

Providers whose APIs use numeric IDs should display the composite
`metadata.name` (e.g. `grafana-instance-health-5594`) as the `NAME` column in
table/wide output. This is the identifier users copy-paste into `get`, `update`,
and `delete` commands. Bare numeric IDs are accepted as input (for backward
compatibility) but should not be the primary display column.

Shared helpers in `internal/resources/adapter/slug.go` —
`SlugifyName`, `ExtractIDFromSlug`, `ComposeName` — implement the slug-id
convention. `SetResourceName` must extract and restore the API-level ID from
the composite name so CRUD operations work after a K8s round-trip.

Reference: Fleet (pipelines, collectors) and Synth (checks) providers.

### K8s Manifest Wrapping `[ADOPT]`

Provider list/get commands that output **CRUD resources** (resources the user can
create, update, and delete via the CLI) must wrap json/yaml output in K8s
envelope manifests (`apiVersion`/`kind`/`metadata`/`spec`) for round-trip
compatibility with push/pull. Table/wide codecs continue to receive raw domain
types for direct field access.

Commands that are **exempt** from K8s wrapping:

| Category | Examples | Rationale |
|----------|----------|-----------|
| Query/search results | `insights query`, `search entities` | Time-series and aggregation results, not storable resources |
| Operational views | `status`, `health`, `inspect` | Composite or derived data, not individual resources |
| Read-only reference data | `vendors list`, `scopes list`, `entity-types list` | Discoverable metadata, not user-managed resources |
| Singleton config | `env get`, `graph-config` | Single config objects, not collections of resources |

### Build Verification `[CURRENT]`

- [ ] `make build` succeeds
- [ ] `make tests` passes with no regressions
- [ ] `make lint` passes
- [ ] `gcx providers` lists the new provider
- [ ] `gcx config view` redacts secrets correctly

---

## 14. Provider / Resources Output Consistency `[ADOPT]`

Provider CRUD commands must use their registered `ResourceAdapter` (via
TypedCRUD) for data access, not raw REST clients. This ensures:

- JSON/YAML output is identical to the `resources` pipeline by construction.
- Table/wide codecs may access domain types `T` for richer columns (e.g.
  SLI%, burn rate, budget remaining).
- The `resources` pipeline uses generic resource columns (name, namespace,
  age) for its table codec.

Provider commands that bypass the adapter for CRUD operations are
non-compliant. Extension commands (status, timeline, etc.) may use raw
clients since they have no `resources` pipeline equivalent.

---

## 15. TypedCRUD Pattern `[ADOPT → EVOLVE]`

TypedCRUD is the current required pattern for new providers implementing
ResourceAdapter. It bridges typed domain objects to Kubernetes-style
unstructured envelopes.

**Current requirement:** New providers must use TypedCRUD for adapter
registration.

**Trajectory:** Domain types should be designed with eventual K8s metadata
interface compliance in mind (metadata.name, metadata.namespace,
apiVersion/kind). The long-term goal is typed resources that satisfy K8s
interfaces directly, eliminating the TypedCRUD bridge.

Do not introduce new serialization bridges, dispatch patterns, or
type-erasure mechanisms. If TypedCRUD does not fit your use case, raise
the issue for architectural discussion.

---

## 16. Provider ConfigLoader `[ADOPT]`

All provider commands must use `providers.ConfigLoader` for flag binding
(`--config`, `--context`) and config resolution (YAML + env var precedence).

### ConfigLoader API

| Method | Purpose | Used by |
|--------|---------|---------|
| `LoadGrafanaConfig(ctx)` | REST config for Grafana API calls | alert, fleet, incidents, kg, oncall, slo, synth |
| `LoadCloudConfig(ctx)` | Cloud token + GCOM stack info | k6, fleet |
| `LoadProviderConfig(ctx, name)` | Provider-specific `map[string]string` + namespace | synth, oncall, k6 |
| `SaveProviderConfig(ctx, name, key, val)` | Write-back a single provider config key | synth (datasource UID) |
| `LoadFullConfig(ctx)` | Full `*config.Config` (for cross-cutting lookups) | synth (datasource discovery) |

### Provider-specific config pattern

Providers that need custom keys (URLs, tokens, domain overrides) use
`LoadProviderConfig` instead of ad-hoc `os.Getenv` or `ProviderConfig` map
access. This ensures `GRAFANA_PROVIDER_<NAME>_<KEY>` env vars, config file
values, and `--context` switching all work uniformly:

```go
// In provider's config loader or adapter factory:
providerCfg, namespace, err := l.LoadProviderConfig(ctx, "synth")
if err != nil {
    return err
}
smURL := providerCfg["sm-url"]  // resolved from env or config file
```

Provider-specific defaults and fallbacks (e.g., `DefaultAPIDomain` for k6,
plugin discovery for oncall) remain in the provider package — `ConfigLoader`
is generic.

### Do not

- Import `cmd/gcx/config` from provider code (import cycle)
- Roll custom flag binding for `--config`/`--context`
- Construct HTTP clients or load credentials outside ConfigLoader
- Hardcode env var names — ConfigLoader handles `GRAFANA_PROVIDER_*` resolution
- Use `os.Getenv` for provider-specific env vars — use `LoadProviderConfig`
- Swallow errors from `LoadProviderConfig` — propagate them; only fall through
  to alternative resolution when the key is absent, not when config loading fails

See [environment-variables.md](environment-variables.md) for the canonical env var reference including `GRAFANA_PROVIDER_*` patterns.
See [naming.md § Config Key Naming](naming.md#93-config-key-naming-current) for naming conventions.
