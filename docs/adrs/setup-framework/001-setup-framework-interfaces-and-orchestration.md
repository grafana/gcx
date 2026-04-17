# Setup Framework: Interfaces and Orchestration

**Created**: 2026-04-16
**Status**: proposed
**Bead**: TBD
**Supersedes**: none

<!-- Status lifecycle: proposed -> accepted -> deprecated | superseded -->

## Context

gcx has 16+ providers but no unified way to answer "what's configured,
what's broken, and what should I do next?" The current `gcx setup status`
is hardcoded to show only instrumentation. Each provider's onboarding is
independent, with no orchestrated path from "just logged in" to "products
working."

The [UX consistency design](docs/plans/2026-04-14-ux-consistency-design.md)
(Area 2) specifies a setup framework with three entry points:

- **`gcx setup status`** — aggregated product status dashboard.
- **`gcx setup run`** — interactive multi-select onboarding orchestrator;
  re-runnable, skips already-configured products.
- **`gcx <provider> setup`** — per-provider non-interactive setup commands.

This ADR decides the interfaces, contracts, and orchestration model.
Detailed implementation (timeouts, package layout, error-message
conventions, codec wiring) belongs in the spec that follows this ADR.

**Key constraint** ([CONSTITUTION.md](CONSTITUTION.md)): providers are
self-registering plugins. The framework must discover setup capabilities
through the existing `providers.All()` registry, not hard-coded lists.

**Scope**: framework interfaces, orchestration model, and stub
implementations on all existing providers so `gcx setup status` shows
every provider from day one. Rich per-provider setup logic (synth auto-
discovery, Faro app creation, etc.) remains Area 7.

**Related**: #287 (unified setup framework), #319 (setup status dashboard),
[docs/adrs/instrumentation/001-instrumentation-provider-design.md](docs/adrs/instrumentation/001-instrumentation-provider-design.md)

## Decision

### 1. Two Optional Interfaces: `StatusDetectable` and `Setupable`

The framework defines two optional interfaces discovered via Go type
assertion on `providers.All()`. `Setupable` embeds `StatusDetectable`,
enforcing at compile time that anything in the run flow also appears in
the status table.

```go
// StatusDetectable is the minimum contract to appear in `gcx setup status`.
// Status-only providers (e.g. signal providers with no setup flow) implement
// just this.
type StatusDetectable interface {
    // ProductName returns the human-readable product name
    // (e.g., "Synthetic Monitoring").
    ProductName() string

    // Status reports the provider's current configuration state.
    // Must respect ctx deadline. "Not configured" is a valid state,
    // not an error.
    Status(ctx context.Context) (*ProductStatus, error)
}

// Setupable is the full contract for providers that participate in
// `gcx setup run`. Setupable embeds StatusDetectable — you cannot set
// up something you cannot describe.
type Setupable interface {
    StatusDetectable

    // ValidateSetup validates params without side effects. Called by
    // the orchestrator before Setup and by the CLI setup command during
    // preflight. Returns nil if params are acceptable. Errors are
    // user-facing and should guide correction.
    ValidateSetup(ctx context.Context, params map[string]string) error

    // Setup runs the provider's setup flow. Called by both the CLI
    // setup command and the `gcx setup run` orchestrator. Providers
    // without a real setup flow are not Setupable — they implement
    // StatusDetectable only.
    Setup(ctx context.Context, params map[string]string) error

    // InfraCategories returns the infrastructure categories this
    // provider handles for `gcx setup run`. Returns nil if the provider
    // does not participate in guided run (but still has a CLI
    // `gcx <provider> setup` command).
    InfraCategories() []InfraCategory

    // ResolveChoices returns dynamic choices for a parameter whose
    // SetupParam has Kind=choice/multi_choice and empty static Choices.
    // Returns nil, nil if the provider has no dynamic choices.
    ResolveChoices(ctx context.Context, paramName string) ([]string, error)
}
```

Providers that don't implement either interface remain fully functional
as regular providers but are invisible to the setup framework.

### 2. `ProductStatus` and `ProductState`

```go
type ProductState string

const (
    StateNotConfigured ProductState = "not_configured"
    StateConfigured    ProductState = "configured"
    StateActive        ProductState = "active"
    StateError         ProductState = "error"
)

type ProductStatus struct {
    Product   string       // same as ProductName()
    State     ProductState
    Details   string       // human-readable summary; may be empty
    SetupHint string       // CLI command to configure; empty when active
}
```

Providers determine state using whatever criteria makes sense for them.
A typical detection combines the provider's existing `ConfigKeys()`
(absent = `not_configured`) with an API probe (unreachable = `error`,
empty resources = `configured`, resources present = `active`). The
framework does not prescribe a waterfall — providers own their state
semantics.

### 3. `gcx setup status` — Aggregation Contract

The status command iterates `providers.All()`, type-asserts
`StatusDetectable`, and calls `Status()` on each in parallel. Per-provider
failures must not block the aggregate:

- Each `Status()` call runs under an enforced per-provider timeout.
- An error from `Status()` is surfaced as a `StateError` row with the
  error message in `Details` — it never cancels other providers' calls.
- Rows are rendered in a deterministic order (alphabetical by product
  name) regardless of call completion order.

The table uses the standard gcx codec system (`text|json|yaml|wide`),
not a bespoke format. Human mode color-codes state
(gray/yellow/green/red). Agent mode suppresses color and truncation.

### 4. `InfraCategory` and `SetupParam`

```go
type InfraCategoryID string

type InfraCategory struct {
    ID     InfraCategoryID
    Label  string        // multi-select label; first provider's label wins
    Params []SetupParam
}

type ParamKind string

const (
    ParamKindText        ParamKind = "text"
    ParamKindBool        ParamKind = "bool"
    ParamKindChoice      ParamKind = "choice"       // single select
    ParamKindMultiChoice ParamKind = "multi_choice" // multi-select
)

type SetupParam struct {
    Name     string
    Prompt   string
    Kind     ParamKind
    Required bool
    Default  string

    // Secret indicates the value is sensitive. The orchestrator masks
    // input during prompting and renders "***" in the preview.
    Secret bool

    // Choices is used only when Kind is choice/multi_choice. If empty
    // and Kind requires choices, the orchestrator calls ResolveChoices.
    Choices []string
}
```

`SetupParam` drives three things:
- Interactive prompt widget and input masking during `setup run`.
- Flag registration on the CLI `gcx <provider> setup` command.
- Preview rendering before confirmation.

### 5. Provider Setup Command Contract

Every provider that implements `Setupable` must expose
`gcx <provider-area> setup` as a Cobra subcommand. The Cobra command
is a thin wrapper around `Setupable.Setup()` — one implementation,
two entry points.

| Requirement | Contract |
|-------------|----------|
| Location | `gcx <provider-area> setup` |
| Interaction | Non-interactive; flags only. No stdin prompts. |
| Structure | Standard opts pattern (opts struct + setup(flags) + Validate + constructor). |
| Idempotency | Safe to re-run. Detects existing configuration and skips or updates. |
| Output | Success: structured mutation summary via codec system. Failure: error to stderr, non-zero exit. |
| Agent-friendly | Must work in agent mode (JSON output, no prompts). |

The CLI command collects flag values, builds the params map (same shape
the orchestrator uses), calls `ValidateSetup`, then `Setup`. Agents invoke
this command directly rather than `gcx setup run`.

### 6. `gcx setup run` — Orchestration Model

The run command is interactive, re-runnable, and idempotent. Subsequent
invocations skip already-configured products and fill in gaps.

Orchestration flow:

1. **Discovery** — collect `InfraCategories()` from every `Setupable`.
   Multiple providers may register the same category ID; the first
   encountered label wins.

2. **Category selection** — present a multi-select of infrastructure
   categories. User picks what they have.

3. **Provider resolution** — map selected categories to the providers
   that handle them. Providers are processed sequentially in
   alphabetical order.

4. **Per-provider loop**:
   - **Skip check** — call `Status()`. If `configured` or `active`,
     print "already configured, skipping" and continue.
   - **Parameter collection** — for each `SetupParam`:
     - If `Kind` is choice/multi_choice and `Choices` is empty, call
       `ResolveChoices` to populate options.
     - Render the input widget matching `Kind` (text / bool / select /
       multi-select). Mask input when `Secret` is set.
     - Collect the value into the params map.
   - **Validation** — call `ValidateSetup`. On failure, print the error
     and re-run parameter collection with previously-collected values as
     defaults. Loop until validation passes or the user cancels.

5. **Preview and confirmation** — before any mutation, display the
   configuration summary:

   ```
   About to configure:

     Synthetic Monitoring
       url:        https://example.com
       frequency:  60s
       probes:     Atlanta, London, Tokyo

     Frontend Observability
       name:       my-app
       sourcemaps: enabled

     OnCall

   Continue? [Y/n]
   ```

   Providers with no parameters show the name only. Secret values render
   as `***`.

6. **Execution** — for each confirmed provider, sequentially:
   - Call `Setup(ctx, params)`. Stream progress to stderr.
   - On error, log and continue to the next provider. Partial completion
     is valid state; re-running `setup run` retries failed providers
     because they will not reach `configured` state.

7. **Summary** — render the status table showing post-run state.

Ctrl-C at any point prints a summary of what completed and what remains,
then exits. Completed setups are not rolled back.

**Agent mode**: `gcx setup run` refuses in agent mode and points to
individual provider setup commands. The orchestrator is inherently
interactive; agents already know the exact flags and should invoke
`gcx <provider> setup` directly.

**Validation retry targeting**: on validation failure, the orchestrator
re-prompts all fields for that provider with previously-collected values
pre-filled as defaults. The user presses Enter to accept fields that
were correct and corrects the offending one. This avoids parsing
validator error messages to target specific fields — a deliberate
simplicity trade-off (see rejected alternatives).

**Concurrency**: `Status()` calls in section 3 are parallel. `Setup()`
calls here are sequential. The two operations have different
trade-offs: `Status()` is read-only, fast, and its output is a single
aggregated table rendered at the end — parallelism is pure win.
`Setup()` has side effects, streams progress to the user, may have
cross-provider ordering concerns (instrumentation → metrics/logs flow),
and is typically invoked for a small N (1–5 providers). Sequential
execution keeps output legible, error attribution clear, and the door
open to future dependency-aware ordering.

### 7. Stub Implementations on Existing Providers

To validate the interfaces and populate the status table from day one,
every existing provider gets a minimal implementation:

- **Signal and data providers without real setup flows** (metrics, logs,
  traces, profiles, etc.) implement `StatusDetectable` only. `Status()`
  returns a basic config-key-presence check using the provider's
  existing `ConfigKeys()`. No API probing in the stub.
- **Providers that will eventually support setup** (synth, frontend,
  appo11y, etc.) implement `Setupable` with `Setup()` returning
  `ErrSetupNotSupported` and `InfraCategories()` returning `nil`. They
  show up in the status table but not yet in `setup run`.

Area 7 then progressively replaces stubs with rich implementations —
real API probing in `Status()`, working `Setup()` flows, populated
`InfraCategories()` — one provider at a time, without framework changes.

## Rejected Alternatives

- **Single combined interface** (`SetupProvider` with all 6 methods):
  initially chosen when the interface had 4 methods, reconsidered once
  `ValidateSetup` and `ResolveChoices` were added. Status-only providers
  would carry four nil-returning stubs out of six. The embedded-interface
  split keeps the status-only case clean while preserving the invariant
  that setupable implies detectable.

- **Generic `SetupProvider[T]`** with typed `Setup(ctx, opts T)`:
  type-erased iteration (`providers.All()`) cannot discover generic
  interfaces. Same wall that led `TypedCRUD[T]` to pair with
  `AsAdapter()` — typed inside a provider, untyped at the framework
  boundary. Providers get type safety via internal opts structs; the
  `map[string]string` is the plugin-uniform envelope.

- **`map[string]any` params**: `any` forces type assertions at every
  call site without adding safety. Setup input originates as strings
  (argv flags, stdin prompts) — `strconv.Atoi` / `time.ParseDuration`
  in the provider's param parser is the right place for typing.
  Matches the existing `Provider.Validate(cfg map[string]string)`
  pattern.

- **Three-way interface split** (StatusDetectable → Setupable →
  WizardCapable): tempting symmetry, but providers almost never have
  a non-wizard setup flow. The two-way split covers the real cases;
  `InfraCategories() == nil` already expresses "setupable but not in
  the guided run."

- **Descriptor-based approach** (provider returns a `SetupDescriptor`
  struct; framework executes probing and command generation): more
  uniform but requires `ProbeFunc` escape hatches for providers with
  unusual detection logic (synth auto-discovery, fleet multi-cluster),
  eroding the uniformity benefit. Method-based interfaces give each
  provider full flexibility while the framework controls orchestration.

- **Extending the `Provider` interface** with setup methods: conflates
  "registered provider" with "setup-capable provider" and forces every
  existing provider to implement setup stubs as part of core `Provider`.
  Optional interfaces via type assertion keep the concerns separate.

- **Subprocess invocation** from the run orchestrator
  (`exec "gcx" "synth" "setup" ...`): adds cold-start overhead, loses
  in-process context and logging, and introduces PATH/binary-location
  fragility. Direct `Setup()` method calls are the right primitive; the
  Cobra command is a thin wrapper around it.

- **Concurrent `Setup()` execution**: small N, cross-provider ordering
  concerns (instrumentation → metrics/logs), progress streaming
  interleaving, and harder error attribution. Sequential execution is
  strictly better for v1. If we later find a real opportunity (two
  provably independent providers each taking 30+ seconds), we add
  `ConcurrencySafe() bool` or a `--parallel` flag — not v1.

- **CLI-command preview** (showing `gcx synth setup --url ...` commands
  before confirmation): power-user feature; default preview shows
  selected products and collected values. Can be surfaced later behind
  `--dry-run` or `--show-commands`.

- **Field-level validation error targeting** (re-prompt only the bad
  field): requires a structured error protocol between
  `ValidateSetup` and the orchestrator. Deferred in favor of
  full-param re-prompting with previous values as defaults. Good
  enough for v1; revisit if providers demand it.

- **Rich choice metadata** (`[]Choice{Value, Description}` from
  `ResolveChoices`): `[]string` is sufficient for v1. Evolve when a
  provider genuinely needs descriptions alongside values.

- **Dependency-aware provider ordering** (e.g., instrumentation before
  metrics): alphabetical is sufficient initially. If ordering needs
  emerge, add a `Dependencies() []string` method or priority field
  on `InfraCategory` — not v1.

## Consequences

### Positive

- **Self-registering**: adding setup to a new provider requires only
  implementing the interface on its existing type.
- **Incremental adoption**: stubs ship on day one; rich implementations
  land provider-by-provider in Area 7 without framework changes.
- **Status-only path is cheap**: providers that don't need setup flows
  implement 2 methods total via `StatusDetectable`.
- **Embedding enforces invariants**: `Setupable` embeds
  `StatusDetectable` at compile time. Can't be setupable without being
  detectable.
- **Agent-friendly**: `Setup()` is directly callable from in-process
  orchestration; `gcx setup status --output json` gives agents a
  structured view; `gcx <provider> setup` is the non-interactive
  entry point agents already want.
- **Error isolation**: one broken provider's `Status()` can't block
  the table or other providers' setup.

### Negative

- **Consistency risk**: each provider owns its `Status()`, `Setup()`,
  and `ValidateSetup()` logic. Without guidelines, semantics may
  drift across providers. Mitigated by code review and the stub-first
  rollout (stubs establish a minimum bar before rich implementations
  land).
- **Two interfaces to understand**: modest learning overhead. Offset
  by the standard Go embedding idiom (`io.ReadWriter` embeds
  `io.Reader` and `io.Writer`).
- **`Setup()` surface area**: `params map[string]string` is a loose
  contract. Providers must document expected keys; the CLI command's
  flags serve as the canonical documentation and the `SetupParam`
  declarations serve as the declarative schema.

### Follow-up Work

- **Spec** for framework implementation (timeouts, package layout,
  error-message conventions, codec wiring) — the concrete plan that
  turns this ADR into code.
- **Area 7**: progressively replace stubs with rich `Status()` probes,
  working `Setup()` flows, populated `InfraCategories()`, and
  `ResolveChoices()` for dynamic wizards.
- **Future**: field-level validation error targeting if providers
  demand it.
- **Future**: `ConcurrencySafe()` or `--parallel` flag if real
  independent-setup opportunities emerge.
- **Future**: `gcx setup doctor` — deeper health checks beyond what
  status shows (token expiry, API version compatibility).
- **Future**: dependency-aware provider ordering if cross-provider
  dependencies become common.
- **Future**: `--dry-run` / `--show-commands` on `setup run` for
  power users who want the equivalent CLI commands.
