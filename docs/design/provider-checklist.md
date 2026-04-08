# Provider Command Checklist

> UX compliance checklist for new providers. Architecture patterns (TypedCRUD, ConfigLoader, output consistency) are in [patterns.md](../architecture/patterns.md).

Extends the interface checklist in [provider-guide.md](../reference/provider-guide.md) with
UX requirements. All items are unless marked otherwise.

---

## 7. Provider Command Checklist

### Interface Compliance

- [ ] Struct implements all five `Provider` interface methods
- [ ] `Name()` is lowercase, unique, and stable (it's the config map key)
- [ ] All config keys are declared in `ConfigKeys()`
- [ ] Secret keys (passwords, tokens, API keys) have `Secret: true`
- [ ] `Validate()` returns error pointing to `gcx config set ...`
- [ ] Provider added to `internal/providers/registry.go:All()`

### UX Compliance

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

### Slug-ID Naming in Tables

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

### K8s Manifest Wrapping

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

### Build Verification

- [ ] `make build` succeeds
- [ ] `make tests` passes with no regressions
- [ ] `make lint` passes
- [ ] `gcx providers` lists the new provider
- [ ] `gcx config view` redacts secrets correctly

---

## Architecture Patterns

Provider architecture patterns (TypedCRUD, ConfigLoader, output consistency) are documented in [patterns.md § Provider Plugin System](../architecture/patterns.md). Those are structural requirements; this file covers UX requirements.
