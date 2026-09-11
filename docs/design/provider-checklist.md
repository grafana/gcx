# Provider Command Checklist

> UX compliance checklist for new providers. Architecture patterns (TypedCRUD, ConfigLoader, output consistency) are in [patterns.md](../architecture/patterns.md).

Extends the interface checklist in [provider-guide.md](../reference/provider-guide.md) with
UX requirements. All items are unless marked otherwise.

---

## 7. Provider Command Checklist

### Interface Compliance

- [ ] Struct implements all six `Provider` interface methods (including `TypedRegistrations()`; `nil` is valid for commands-only providers)
- [ ] `Name()` is lowercase, unique, and stable (it's the config map key)
- [ ] All config keys are declared in `ConfigKeys()`
- [ ] Secret keys (passwords, tokens, API keys) have `Secret: true`
- [ ] `Validate()` returns error pointing to `gcx config set ...`
- [ ] Provider self-registers via a single `providers.Register()` in `init()` + blank import in `cmd/gcx/root/command.go` (no separate `adapter.Register()` calls)

### UX Compliance

- [ ] All data-display commands support `-o json/yaml` (inherited from `io.Options`)
- [ ] List commands default to a narrow table codec (`text` or `table`, matching siblings); get commands may default to `yaml` for editable single objects (see [output.md § 11](output.md#11-codec-requirements-by-command-type))
- [ ] A `wide` codec exists only where it shows columns the narrow view omits — hand-registered, or automatic via `RegisterTableAs` for `Table[T]` commands (see [output.md § 11](output.md))
- [ ] Error messages include actionable suggestions with exact CLI commands
- [ ] No `os.Exit()` calls in command code — return errors, let `handleError` exit
- [ ] Status messages use `cmdio.Success/Warning/Error/Info`, written to `cmd.ErrOrStderr()`
- [ ] `--config` and `--context` inherited via `configOpts` persistent flags
- [ ] Mutating commands document whether and how they support `--dry-run`; never imply support inherited from another command
- [ ] Help text follows [help-text.md](help-text.md) standards (Short/Long/Examples)
- [ ] New canonical verbs and command placement follow [command-naming.md](command-naming.md);
  uncovered verbs or identity shapes have explicit maintainer review
- [ ] Push-like operations are idempotent (create-or-update)
- [ ] Data fetching is format-agnostic — do not gate fetches on `--output` value (Pattern 13)
- [ ] PromQL queries use `promql-builder` (`github.com/grafana/promql-builder/go/promql`), not string formatting (Pattern 14)
- [ ] HTTP clients follow [provider-guide Step 4b](../reference/provider-guide.md#step-4b-http-client-construction): preserve the configured Grafana transport for requests to `cfg.Host`, and use `httputils.NewDefaultClient(ctx)` for direct requests to other hosts. No bare `http.Client{}` or `http.DefaultClient`.
- [ ] New list/get commands for adapter-registered resources use K8s envelope manifests in structured formats, including singletons (see below for compatibility scope)
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

- [ ] New list/get commands for registered adapters use the registered resource
  envelope in structured formats, including singleton and read-only adapters.
- [ ] Provider-only query/view results keep their domain representation.
- [ ] Existing output contracts remain compatible; adapter registration does
  not authorize silently rewrapping a released raw result.

See [patterns.md § 17](../architecture/patterns.md#17-k8s-envelope-wrapping-for-provider-listget)
for the decision table, implementation references, and k6 env-var exception.

### Build Verification

- [ ] `mise run build` succeeds
- [ ] `mise run tests` passes with no regressions
- [ ] `mise run lint` passes
- [ ] `gcx providers list` lists the new provider
- [ ] `gcx config view` redacts secrets correctly

---

## Architecture Patterns

Provider architecture patterns (TypedCRUD, ConfigLoader, output consistency) are documented in [patterns.md § Provider Plugin System](../architecture/patterns.md). Those are structural requirements; this file covers UX requirements.
