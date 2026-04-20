---
type: feature-tasks
title: "Setup Framework: Interfaces, Stubs, and Orchestration"
status: approved
spec: docs/specs/feature-setup-framework/spec.md
plan: docs/specs/feature-setup-framework/plan.md
generated: 2026-04-20
---

# Implementation Tasks

Derived from `plan.md` §Implementation Sequence. Waves computed from inter-task
dependencies so independent tasks run in parallel.

> **Provider naming note:** spec.md §Stub Implementations lists providers by
> conceptual name ("incidents"). The actual Go package and `Name()` value for the
> Incidents provider is `irm` (see `internal/providers/irm/` — `IRMProvider.Name()`
> returns `"irm"`). All tasks below MUST use directory and provider names as they
> exist in the repo. The 14 providers are: `alert`, `appo11y`, `faro`, `fleet`,
> `irm`, `k6`, `kg`, `logs`, `metrics`, `profiles`, `sigil`, `slo`, `synth`,
> `traces`.

## Dependency Graph

```
T1 (framework scaffold)
 ├──→ T2 (AggregateStatus)     ──┐
 │                               │
 ├──→ T4 (prompt widgets)     ──┐│
 │                              ││
 ├──→ T7 (signal stubs, 5p)     ││
 │                              ││
 └──→ T8 (setup-cap stubs, 9p)  ││
                                ▼▼
                    T3 (`gcx setup status` CLI)     (needs T2)
                         │
                         │
                    T5 (Orchestrator.Run)          (needs T2, T4)
                         │
                         ▼
                    T6 (`gcx setup run` CLI)       (needs T5)
                         │
                         ▼
                    T9 (docs regen + make all)     (needs T3, T6, T7, T8)
```

**Waves** (topological order, tasks in the same wave run in parallel):

- **Wave 1**: T1
- **Wave 2**: T2, T4, T7, T8
- **Wave 3**: T3, T5
- **Wave 4**: T6
- **Wave 5**: T9

---

## Wave 1: Framework Scaffold

### T1: Scaffold `internal/setup/framework/` package

**Priority**: P0
**Effort**: Medium-Large
**Depends on**: none
**Type**: task

Create the new `internal/setup/framework/` package with interface definitions,
discovery helpers, the `ConfigKeysStatus` stub helper, and the fake-provider
test doubles that every downstream wave will reuse. This is the foundation —
no CLI wiring, no aggregation, no orchestrator yet.

**Deliverables:**

- `internal/setup/framework/interfaces.go`
  - `StatusDetectable` interface: `ProductName() string`, `Status(ctx) (*ProductStatus, error)`.
  - `Setupable` interface: embeds `StatusDetectable`, adds `ValidateSetup(ctx, params) error`,
    `Setup(ctx, params) error`, `InfraCategories() []InfraCategory`,
    `ResolveChoices(ctx, paramName) ([]string, error)`.
  - `ProductState` string enum with exactly four values: `not_configured`,
    `configured`, `active`, `error` (as named constants
    `StateNotConfigured`, `StateConfigured`, `StateActive`, `StateError`).
  - `ProductStatus` struct: `Product`, `State`, `Details`, `SetupHint`.
  - `InfraCategoryID` string alias.
  - `InfraCategory` struct: `ID`, `Label`, `Params`.
  - `ParamKind` string enum with exactly four values: `text`, `bool`, `choice`,
    `multi_choice` (as named constants).
  - `SetupParam` struct: `Name`, `Prompt`, `Kind`, `Required`, `Default`,
    `Secret`, `Choices`.
  - `ErrSetupNotSupported` sentinel error exported as a package-level `var`.

- `internal/setup/framework/discovery.go`
  - `DiscoverStatusDetectable() []StatusDetectable` — iterates `providers.All()`,
    filters via type assertion, returns in arbitrary order.
  - `DiscoverSetupable() []Setupable` — same pattern for `Setupable`.

- `internal/setup/framework/stub.go`
  - `ConfigKeysStatus(p providers.Provider) ProductStatus` — returns
    `StateConfigured` when every non-secret required key in `p.ConfigKeys()`
    has a non-empty value in the active config context; otherwise
    `StateNotConfigured`. `Details` indicates "config keys present; no API probe
    performed" (or similar clarifying copy). MUST NOT perform any API probe.

- `internal/setup/framework/fakes_test.go` (or `internal/setup/framework/testhelpers/`):
  - `FakeStatusDetectable` and `FakeSetupable` test doubles with configurable
    state, errors, latency, and panic behavior. These doubles will be reused
    by T2, T3, T5, T6.

- `internal/setup/framework/registry_test.go` (or similar):
  - `SetupTestRegistry(t *testing.T, providers []providers.Provider)` helper
    that swaps the backing slice of `providers.All()` for the duration of a
    test, restoring via `t.Cleanup`. Guard against concurrent test runs by
    documenting that integration users must disable parallelism or scope
    per-subtest.

- `internal/setup/framework/interfaces_test.go`, `discovery_test.go`,
  `stub_test.go` — table-driven unit tests covering:
  - `StatusDetectable` and `Setupable` compile-time assertions against the
    fake doubles.
  - Discovery returns exactly the providers that implement each interface.
  - `ConfigKeysStatus` returns `StateConfigured` iff every non-secret required
    key has a value; `StateNotConfigured` otherwise.
  - Secret keys are ignored by the heuristic.

**Acceptance criteria:**

- GIVEN the provider registry contains providers A (StatusDetectable),
  B (plain Provider), C (Setupable)
  WHEN `DiscoverStatusDetectable()` is called
  THEN it returns both A and C (since `Setupable` embeds `StatusDetectable`),
  and does NOT return B.

- GIVEN the provider registry contains providers A (StatusDetectable) and
  C (Setupable)
  WHEN `DiscoverSetupable()` is called
  THEN it returns only C.

- GIVEN a provider with `ConfigKeys() = [{Name: "url", Secret: false},
  {Name: "token", Secret: true}]` and the active context has a value for "url"
  WHEN `ConfigKeysStatus(p)` is called
  THEN it returns `ProductStatus{Product: p.Name(), State: StateConfigured,
  Details: "…"}`.

- GIVEN the same provider but the active context has NO value for "url"
  WHEN `ConfigKeysStatus(p)` is called
  THEN it returns `ProductStatus{Product: p.Name(), State: StateNotConfigured,
  Details: "…"}`.

- GIVEN `ErrSetupNotSupported`
  WHEN an external caller tests with `errors.Is(err, framework.ErrSetupNotSupported)`
  THEN identity is preserved across wrap/unwrap.

- `go vet ./internal/setup/framework/...` passes; `go test ./internal/setup/framework/...`
  passes with `-race`; no import cycle between `internal/setup/framework` and
  `internal/providers`.

**Implementation notes:**

- `internal/setup/framework` imports `internal/providers` for the base
  `Provider` interface and `providers.All()`. `internal/providers` MUST NOT
  import `internal/setup/framework`. Enforce with a build-time cycle check.
- No changes to `internal/providers/provider.go` — the base interface is
  untouched (FR-009).
- The `SetupTestRegistry` helper must avoid mutating `providers.All()` if the
  registry type is opaque. If needed, expose a test-only setter via a
  `go:linkname`-free mechanism (e.g., a test hook registered from
  `internal/providers`).

---

## Wave 2: Aggregation, Prompts, and Stubs (parallel)

### T2: Implement `framework.AggregateStatus`

**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Implement the parallel, error-isolated status aggregator that every downstream
consumer (CLI `setup status`, orchestrator pre-run check, post-run summary)
calls.

**Deliverables:**

- `internal/setup/framework/aggregate.go`
  - `AggregateStatus(ctx context.Context, timeout time.Duration) []ProductStatus`
  - Uses `golang.org/x/sync/errgroup` with `SetLimit(10)` for bounded parallelism.
  - Each `Status()` call runs under its own `context.WithTimeout(ctx, timeout)`.
  - `Status()` errors are converted into `ProductStatus{State: StateError,
    Details: err.Error()}` (error isolation — sibling calls are not cancelled).
  - Timeouts are rendered as `StateError` with a timeout message.
  - Panics in `Status()` are recovered and rendered as `StateError` with a
    panic message (never crash the command).
  - Returned slice is sorted alphabetically by `ProductName()` before return.
  - `timeout <= 0` falls back to 5 seconds (the spec-declared default).

- `internal/setup/framework/aggregate_test.go` — table-driven tests covering:
  - Happy path: three providers with distinct states, alphabetical rendering,
    completion-order independence.
  - Error isolation: one provider errors, others succeed.
  - Timeout: one provider exceeds its timeout, others complete under it.
  - Panic: one provider panics, others succeed.
  - Mixed: errors + timeouts + successes in the same call.
  - Ordering: call-completion order C, A, B → rendered order A, B, C.

**Acceptance criteria:**

- GIVEN three `StatusDetectable` fakes returning `StateActive`, `StateConfigured`,
  `StateNotConfigured` (in completion-time order C, A, B)
  WHEN `AggregateStatus(ctx, 5s)` returns
  THEN the result is alphabetically ordered by ProductName.

- GIVEN three providers, one of which returns a non-nil error from `Status()`
  WHEN `AggregateStatus` returns
  THEN the erroring provider appears as `StateError` with the error message in
  `Details` and the other two provider rows reflect their real state (no sibling
  cancellation).

- GIVEN a provider whose `Status()` sleeps for 10s with a 5s per-provider timeout
  WHEN `AggregateStatus(ctx, 5s)` is called
  THEN the provider's row is `StateError` with a timeout-indicating `Details`,
  AND the call returns in ≤ ~6s (bounded by the timeout, not the sleep).

- GIVEN a provider whose `Status()` panics with a runtime error
  WHEN `AggregateStatus` runs
  THEN the provider's row is `StateError` with a panic-indicating `Details`,
  AND other providers return their real state.

- GIVEN ten `StatusDetectable` fakes
  WHEN `AggregateStatus` is called
  THEN the bounded parallelism is respected (no more than 10 in-flight calls;
  verify via counter in the fake).

**Implementation notes:**

- Use `context.WithTimeout` inside the errgroup closure, not outside, so each
  provider gets its own deadline independent of siblings.
- `errgroup.Group.Wait()` is called without returning its error — the aggregator
  does not propagate individual provider errors upward; they are folded into the
  result slice.

---

### T4: Implement prompt widgets

**Priority**: P0
**Effort**: Medium
**Depends on**: T1
**Type**: task

Implement the minimal interactive prompt widgets used by the orchestrator.
Must work over a `bufio.Reader` input and `io.Writer` output, with optional
raw-mode masking for secret input via `golang.org/x/term`.

**Deliverables:**

- `internal/setup/framework/prompt/prompt.go`
  - `Text(in io.Reader, out io.Writer, prompt, def string) (string, error)` —
    line-edited text input with default. Empty input returns `def`.
  - `Bool(in, out, prompt string, def bool) (bool, error)` — `[Y/n]` or `[y/N]`
    depending on `def`; yes/no parsing tolerates `y`, `Y`, `yes`, `n`, `N`, `no`.
  - `Choice(in, out, prompt string, options []string, def string) (string, error)` —
    numbered menu, Enter accepts `def` if non-empty, arrow-key nav NOT required
    (number-entry sufficient for v1).
  - `MultiChoice(in, out, prompt string, options []string, defs []string) ([]string, error)` —
    numbered menu with comma-separated selection input ("1,3,5"), Enter accepts
    the defaults.
  - `Secret(in *os.File, out io.Writer, prompt string) (string, error)` —
    uses `term.MakeRaw` + `term.ReadPassword` for masked input. MUST `defer
    term.Restore(fd, state)` to guarantee terminal state is restored on panic.

- `internal/setup/framework/prompt/prompt_test.go` — tests cover:
  - Text widget: default returned on empty input; entered value returned otherwise.
  - Bool widget: Y/N/y/n/yes/no all parsed; empty returns default.
  - Choice widget: out-of-range index re-prompts; Enter accepts default.
  - MultiChoice widget: comma-separated selection; invalid index re-prompts;
    Enter accepts defaults.
  - Secret widget: raw-mode terminal state is restored even if the read panics.
  - Required-with-no-default: empty input re-prompts.

**Acceptance criteria:**

- GIVEN a `Text` prompt with default "foo" and empty user input
  WHEN the prompt returns
  THEN the returned value is "foo".

- GIVEN a `Secret` prompt that panics mid-read
  WHEN the panic is recovered in the test
  THEN the terminal's raw-mode state has been restored.

- GIVEN a `Choice` prompt with options `[a, b, c]` and default `b`
  WHEN the user presses Enter without typing
  THEN the prompt returns `b`.

- GIVEN a `MultiChoice` prompt with options `[a, b, c, d]` and defaults `[]`
  WHEN the user types `1,3`
  THEN the prompt returns `[a, c]`.

- `go test ./internal/setup/framework/prompt/...` passes with `-race`.

**Implementation notes:**

- Do NOT adopt `bubbletea`, `survey/v2`, or any heavy TUI library. Widget
  implementations are in-repo with `bufio` + `x/term`.
- Non-TTY input detection is the orchestrator's concern (T5), not the widget's.
  Widgets accept arbitrary `io.Reader`/`io.Writer` so tests can drive them
  with `bytes.Buffer`.

---

### T7: Add `StatusDetectable` stubs to signal providers

**Priority**: P1
**Effort**: Medium
**Depends on**: T1
**Type**: task

Add `ProductName()` and `Status()` methods to the 5 signal providers. Each
`Status()` delegates to `framework.ConfigKeysStatus`. No API probing.

**Providers**: `alert`, `logs`, `metrics`, `profiles`, `traces`.
(Dirs: `internal/providers/{alert,logs,metrics,profiles,traces}/`.)

**Deliverables:**

- `internal/providers/<area>/provider.go` (modified × 5)
  - Add methods to the existing provider struct:
    - `ProductName() string` — returns the provider's display name
      (same as `Name()` for v1; future work may differentiate).
    - `Status(ctx context.Context) (*framework.ProductStatus, error)` — calls
      `framework.ConfigKeysStatus(p)` and returns it.

- `internal/providers/<area>/provider_test.go` (modified × 5)
  - Assert the provider implements `framework.StatusDetectable`
    (via compile-time type assertion or runtime check).
  - Assert it does NOT implement `framework.Setupable`
    (runtime `_, ok := p.(framework.Setupable); !ok`).
  - Assert `ProductName()` matches `Name()`.
  - Exercise `Status(ctx)` with a provider whose config keys are fully set → `StateConfigured`.
  - Exercise `Status(ctx)` with missing config keys → `StateNotConfigured`.

**Acceptance criteria:**

- GIVEN the metrics provider whose required config keys are all present
  WHEN `provider.Status(ctx)` is called
  THEN it returns `ProductStatus{State: StateConfigured, ...}`.

- GIVEN the logs provider with missing required config keys
  WHEN `provider.Status(ctx)` is called
  THEN it returns `ProductStatus{State: StateNotConfigured, ...}`.

- `go vet ./internal/providers/{alert,logs,metrics,profiles,traces}/...` passes.
- `go test ./internal/providers/{alert,logs,metrics,profiles,traces}/...` passes.
- Compile-time assertion that signal providers implement `StatusDetectable` but
  NOT `Setupable`.

---

### T8: Add `Setupable` stubs + per-provider setup commands

**Priority**: P1
**Effort**: Large
**Depends on**: T1
**Type**: task

Add the full `Setupable` stub plus a per-provider `setup` Cobra subcommand
to the 9 setup-capable providers. All stubs return `ErrSetupNotSupported`
from `Setup()`; the Cobra commands surface that sentinel with a "not yet
implemented" diagnostic and a non-zero exit.

**Providers**: `appo11y`, `faro`, `fleet`, `irm`, `k6`, `kg`, `sigil`, `slo`,
`synth`.
(Directories: `internal/providers/{appo11y,faro,fleet,irm,k6,kg,sigil,slo,synth}/`.)

> **Naming note**: spec FR-048 says "incidents" for the Incidents provider;
> the actual directory and `Name()` value is `irm`. Use `irm` everywhere in
> this task.

**Deliverables:**

- `internal/providers/<area>/provider.go` (modified × 9)
  - Add to the existing provider struct:
    - `ProductName() string` — typically returns `Name()`.
    - `Status(ctx) (*framework.ProductStatus, error)` → calls
      `framework.ConfigKeysStatus(p)`.
    - `InfraCategories() []framework.InfraCategory` → returns `nil`.
    - `ResolveChoices(ctx, paramName string) ([]string, error)` → returns
      `(nil, nil)`.
    - `ValidateSetup(ctx, params map[string]string) error` → returns `nil`.
    - `Setup(ctx, params map[string]string) error` → returns
      `framework.ErrSetupNotSupported`.

- `internal/providers/<area>/setup.go` **(new × 9)**
  - Thin Cobra subcommand `setup` using the codebase opts pattern:
    `opts` struct + `setup(flags *pflag.FlagSet)` + `Validate()` + constructor
    returning `*cobra.Command`.
  - Flag-only, non-interactive, idempotent, agent-friendly.
  - `Use: "setup"`, `Short: "Set up <product> (not yet implemented)."`.
  - RunE calls the provider's `Setup(ctx, params)` directly (in-process).
  - On `errors.Is(err, framework.ErrSetupNotSupported)`: stderr message "setup
    not yet implemented for <product>"; exit with non-zero code (exit code 1,
    general). Wire through `cmd/gcx/fail` if that's the codebase convention.
  - Register the command via `provider.Commands()`.

- `internal/providers/<area>/provider_test.go` (modified × 9)
  - Compile-time assertion: the provider implements `framework.Setupable`.
  - `Setup()` returns an error satisfying `errors.Is(err, framework.ErrSetupNotSupported)`.
  - `InfraCategories()` returns `nil`.
  - `ResolveChoices(ctx, "any")` returns `(nil, nil)`.
  - `ValidateSetup(ctx, any)` returns `nil`.
  - Cobra command exists: `provider.Commands()` contains a child named `setup`.
  - Running the `setup` subcommand with `--help` works; running it without
    `--help` exits non-zero and writes the "not yet implemented" diagnostic to
    stderr.

- `internal/providers/<area>/setup_test.go` **(new × 9)** OR fold into
  `provider_test.go` — integration-style test that invokes the Cobra command
  with `cmd.SetArgs([])` + `cmd.SetErr(&buf)` and asserts the stderr message
  and exit behavior.

**Acceptance criteria:**

- GIVEN the `synth` provider
  WHEN `errors.Is(synth.Setup(ctx, nil), framework.ErrSetupNotSupported)`
  THEN it returns `true`.

- GIVEN any of the 9 setup-capable providers
  WHEN `_, ok := p.(framework.Setupable)`
  THEN `ok` is `true`.

- GIVEN `gcx synth setup` is invoked without special flags
  WHEN the command runs
  THEN stderr contains "not yet implemented" (or a substring asserting
  ErrSetupNotSupported messaging) AND the exit code is non-zero.

- GIVEN `gcx synth setup --help` is invoked
  WHEN the help text renders
  THEN it includes a "not yet implemented" marker.

- `go test ./internal/providers/{appo11y,faro,fleet,irm,k6,kg,sigil,slo,synth}/...`
  passes.

**Implementation notes:**

- DO NOT create a framework-level helper that auto-registers the setup command
  for every provider. Each provider registers its own `setup` command for
  v1, matching the existing pattern for Commands().
- The help-tree entry for `gcx <provider-area> setup` will be picked up
  automatically when the Cobra command is added (cmd/gcx/helptree/ scans the
  tree dynamically). Verify at T9 during docs regen.
- If a provider already has a `setup` subcommand (it shouldn't — do a
  preflight check), that's a blocker: open a discussion before overwriting.

---

## Wave 3: CLI Wiring and Orchestrator (parallel)

### T3: Rewrite `gcx setup status` over `AggregateStatus`

**Priority**: P0
**Effort**: Medium
**Depends on**: T2
**Type**: task

Replace the hardcoded instrumentation-only `setup status` body with a call to
`framework.AggregateStatus`, and wire `internal/output.Options` for
`text|json|yaml|wide` codec support. Adds agent-mode JSON default.

**Deliverables:**

- `cmd/gcx/setup/command.go` (modified)
  - Replace the existing `newStatusCommand(loader)` body:
    - Accept `internal/output.Options` — register a custom `text` codec that
      uses `internal/style.TableBuilder` (columns: PRODUCT, STATE, DETAILS,
      HINT) with state-color mapping (gray = not_configured, yellow = configured,
      green = active, red = error). Color only when stdout is a TTY.
    - JSON / YAML codecs use `[]ProductStatus` directly via the default
      encoders.
    - Agent-mode: when `agent.IsAgentMode()` returns true, default `--output`
      to `json`, suppress color, suppress truncation.
    - Call `framework.AggregateStatus(ctx, 5*time.Second)` and feed the result
      through the registered codec.
    - Preserve the existing `loader.BindFlags` for context-loading flags.
  - Remove `setupProductRow` and `writeSetupStatusTable` (replaced by codec).

- `cmd/gcx/setup/command_test.go` **(new — integration tests)**
  - Use `framework.SetupTestRegistry` to inject two `StatusDetectable` fakes.
  - Assert `text` output contains expected rows and column ordering.
  - Assert `json` output is a valid JSON array with keys
    `product`, `state`, `details`, `setup_hint`.
  - Assert `yaml` output matches the JSON variant structure.
  - Assert agent-mode defaults to JSON.
  - Assert an errored provider renders as `StateError` in text/json/yaml.

- Regression verification: `cmd/gcx/setup/instrumentation/status_test.go` must
  continue to pass UNCHANGED. This is a hard gate.

**Acceptance criteria:**

- GIVEN the registry contains providers A (StatusDetectable),
  B (plain Provider), C (StatusDetectable)
  WHEN `gcx setup status --output text` runs
  THEN the output contains rows for A and C in alphabetical order and NO row for B.

- GIVEN three providers `Logs` (active), `Metrics` (configured), `Traces` (not_configured)
  WHEN `gcx setup status --output text` runs on a TTY
  THEN the rows appear alphabetically (Logs, Metrics, Traces) with
  colored state cells (green / yellow / gray).

- GIVEN the same three providers
  WHEN `gcx setup status --output json` runs
  THEN stdout is a JSON array with three objects, each with keys
  `product`, `state`, `details`, `setup_hint`; no ANSI escape codes anywhere.

- GIVEN `gcx setup status` is run with `agent.IsAgentMode()` returning true
  WHEN no `--output` flag is passed
  THEN the command defaults to JSON output, suppresses color, suppresses
  truncation.

- `cmd/gcx/setup/instrumentation/status_test.go` passes WITHOUT modification.

**Implementation notes:**

- The custom text codec registration pattern is documented in
  `docs/architecture/patterns.md` (Pattern 13: format-agnostic fetching).
- Column naming: prefer `PRODUCT | STATE | DETAILS | HINT` over the legacy
  `PRODUCT | ENABLED | HEALTH | DETAILS` — state replaces enabled/health.
- Handle the "no StatusDetectable providers registered" edge case: render an
  empty table with headers and a helpful `Details` note if none discovered,
  OR exit 0 with a stderr note — prefer the empty table (matches gcx UX).

---

### T5: Implement `framework.Orchestrator` (Run)

**Priority**: P0
**Effort**: Large
**Depends on**: T2, T4
**Type**: task

Implement the full `gcx setup run` flow inside the framework. The CLI layer
(T6) is a thin wrapper that calls this. Owns Ctrl-C handling, validation retry,
preview masking, sequential `Setup()`, and the post-run summary.

**Deliverables:**

- `internal/setup/framework/orchestrator.go`
  - `type Options struct { In io.Reader; Out, Err io.Writer; StatusTimeout time.Duration; … }`
  - `type Summary struct { Completed, Failed, Skipped, Cancelled []string; … }`
  - `func Run(ctx context.Context, opts Options) (Summary, error)`
  - Flow per spec §`gcx setup run` orchestration (FR-024..FR-038):
    1. Discover `Setupable` providers via `DiscoverSetupable()`.
    2. Collect all `InfraCategories()` — dedupe by `InfraCategoryID` with
       "first label wins" in alphabetical-ProductName order.
    3. If zero categories → write "no interactive setup flows available" to
       stderr, exit with code 0.
    4. Present a `MultiChoice` of category labels; user picks 0..N.
    5. Resolve providers whose categories intersect the selection; process
       alphabetically by `ProductName()`.
    6. For each provider:
       a. Call `Status(ctx)`. If `StateConfigured` or `StateActive`, stderr
          "already configured, skipping"; add to Summary.Skipped; continue.
       b. For each `InfraCategory` of the selected set, for each `SetupParam`:
          - Render the widget matching `Kind` (text/bool/choice/multi_choice).
          - `Secret=true` masks input via `prompt.Secret`.
          - If `Choices` empty and `Kind ∈ {choice, multi_choice}`, call
            `ResolveChoices(ctx, param.Name)` to populate options.
       c. Call `ValidateSetup(ctx, params)`. On error: stderr the message,
          re-prompt every param for this provider with previously-collected
          values as defaults. Loop until `ValidateSetup` returns nil OR
          user cancels.
    7. Render preview: for each confirmed provider, list params with
       secret-masked values (`***` where `Secret=true`).
    8. Confirmation prompt `Continue? [Y/n]`. Negative → exit code 5,
       summary includes remaining as Cancelled.
    9. Sequentially invoke `provider.Setup(ctx, params)` for each confirmed
       provider; stream progress to stderr. `Setup()` errors → stderr; add
       to Summary.Failed; continue to next provider.
    10. Post-run: call `AggregateStatus(ctx, timeout)` and render via the
        same mechanism as `gcx setup status`.
  - SIGINT handling via `signal.NotifyContext(ctx, os.Interrupt)`: on
    cancellation, stop param collection / next `Setup()` at the next
    checkpoint, print the summary, return exit code 5.
  - Non-TTY stdin detection at Run entry (via `internal/terminal.IsPiped`):
    if stdin is not a TTY, return a usage error with an actionable message.

- `internal/setup/framework/orchestrator_test.go` — fake-provider tests
  covering:
  - Zero categories available → stderr message + exit 0.
  - Skip-if-configured: provider returning `StateActive` is skipped; no
    param prompts; no `Setup()` call.
  - Validation retry: first `ValidateSetup` fails, second succeeds; param
    defaults re-populate previous values.
  - Preview masking: `Secret=true` param value never appears in stdout/stderr
    in the preview block.
  - Sequential execution: `Setup()` call order is alphabetical by ProductName.
  - `Setup()` error isolation: one provider's error does not prevent
    subsequent providers' `Setup()` from running; Summary reflects it.
  - Ctrl-C: simulated cancellation → summary + exit code 5; no rollback
    attempted.
  - Non-TTY stdin refusal.

**Acceptance criteria:**

- GIVEN three `Setupable` fakes in category "kubernetes" all `StateNotConfigured`
  WHEN `Run` is driven with category "kubernetes" selected, valid params for
  each, and confirmed preview
  THEN `Setup()` is invoked alphabetically for all three, Summary.Completed
  lists all three, and the returned error is nil.

- GIVEN a provider returning `StateActive` from `Status()`
  WHEN `Run` reaches the per-provider loop
  THEN stderr contains "already configured, skipping"; `Setup()` is NOT called
  for it; Summary.Skipped includes it.

- GIVEN a provider whose first `ValidateSetup` returns an error
  WHEN the orchestrator receives the error
  THEN stderr prints the error; params are re-prompted with previous values
  as defaults; loop continues until `ValidateSetup` returns nil.

- GIVEN a `SetupParam{Secret: true}` with user value "supersecret"
  WHEN the preview block renders
  THEN the preview prints `***` for that param; "supersecret" does NOT appear
  anywhere in stdout/stderr captured by the test.

- GIVEN two providers have completed `Setup()` and a third is mid-way when
  SIGINT fires
  WHEN the signal is observed
  THEN the summary lists two completed and one remaining, return value indicates
  cancellation, and no rollback was attempted on the completed providers.

- GIVEN stdin is piped (non-TTY)
  WHEN `Run` is called
  THEN it returns a usage error pointing to per-provider `gcx <area> setup`
  without prompting.

**Implementation notes:**

- `signal.NotifyContext` must be scoped to the Run call, not global, to avoid
  leaking handlers across tests.
- Widget calls feed through `opts.In`/`opts.Out` so tests can drive with
  buffers instead of an actual TTY.
- DO NOT short-circuit the post-run status render on Ctrl-C. The summary (not
  the status table) is what ships on cancellation per spec.
- Non-TTY detection for stdin uses `internal/terminal.IsPiped` — assumes a
  facility exists for stdin; if the existing util is stdout-only, add a
  stdin variant here.

---

## Wave 4: `gcx setup run` CLI

### T6: Add `gcx setup run` command

**Priority**: P0
**Effort**: Small-Medium
**Depends on**: T5
**Type**: task

Add the Cobra command `gcx setup run` as a thin wrapper over
`framework.Run`, with early agent-mode refusal and the opts pattern.

**Deliverables:**

- `cmd/gcx/setup/run.go` **(new)**
  - `type runOpts struct { loader *providers.ConfigLoader; … }`
  - `(o *runOpts) setup(flags *pflag.FlagSet)` — register flags (if any;
    likely none for v1).
  - `(o *runOpts) Validate() error`.
  - `newRunCommand(loader *providers.ConfigLoader) *cobra.Command` returning
    a Cobra command:
    - `Use: "run"`, `Short: "Interactive orchestrator for product setup."`.
    - RunE:
      1. If `agent.IsAgentMode()` → write to stderr "interactive setup is
         not available in agent mode; run 'gcx <area> setup' for each
         product" → return a usage error with exit code 2 (via
         `cmd/gcx/fail` if that's the convention).
      2. Call `framework.Run(ctx, Options{In: cmd.InOrStdin(), Out:
         cmd.OutOrStdout(), Err: cmd.ErrOrStderr(), ...})`.
      3. On cancellation, exit with code 5.

- `cmd/gcx/setup/command.go` (modified)
  - Register `newRunCommand(loader)` alongside the existing commands:
    `cmd.AddCommand(newRunCommand(loader))`.

- `cmd/gcx/setup/run_test.go` **(new — integration tests)**
  - Agent-mode refusal: set `agent.IsAgentMode()` → true; assert stderr
    contains the redirect message; assert exit code 2.
  - Full orchestrated flow against `framework.SetupTestRegistry`-injected
    fakes + `bytes.Buffer` stdin/stdout: verify that a happy-path run
    exits 0 with the post-run status table.
  - Zero categories → exit 0 with stderr note.

**Acceptance criteria:**

- GIVEN `agent.IsAgentMode()` returns true
  WHEN `gcx setup run` is invoked
  THEN stderr contains a redirect message to per-provider `gcx <area> setup`
  AND the exit code is 2 AND NO prompts are issued.

- GIVEN a fake registry with one `Setupable` category and a valid preview
  confirmation
  WHEN `gcx setup run` is invoked in non-agent mode
  THEN exit code is 0 and the post-run status table is rendered.

- GIVEN no `Setupable` providers register any categories
  WHEN `gcx setup run` is invoked
  THEN stderr contains "no interactive setup flows available" AND exit code is 0.

**Implementation notes:**

- Reuse `providers.ConfigLoader` so `run` picks up the same context flags as
  `status` (context / namespace / kubeconfig equivalents).
- Exit-code convention: 0 success, 2 usage (agent-mode refusal, flag error),
  5 cancelled (Ctrl-C or preview-no). Map any cancellation error from the
  framework to exit 5; map agent-mode refusal to exit 2.

---

## Wave 5: Docs + Verification

### T9: Regenerate reference docs and run full verification

**Priority**: P0
**Effort**: Small
**Depends on**: T3, T6, T7, T8
**Type**: chore

Regenerate CLI reference docs, env-var reference, config reference, and
linter-rules reference after all command additions. Run full lint/test/build
with agent-mode forced off.

**Deliverables:**

- Regenerated artifacts (auto-updated by `make reference`):
  - `docs/reference/cli/` (new subcommands: `gcx setup run`,
    `gcx <provider-area> setup` × 9).
  - `docs/reference/` env-var / config / linter-rules snapshots if touched.
- Any additional doc-maintenance updates flagged in
  `docs/reference/doc-maintenance.md` (package map in CLAUDE.md if
  `internal/setup/framework/` needs to be listed; ARCHITECTURE.md ADR index
  should already reference ADR-001).
- A release-note line for `CHANGELOG.md` or `.release-notes.md` capturing
  "gcx setup status: output shape now aggregates every StatusDetectable
  provider; instrumentation-only table moved to `gcx setup instrumentation
  status`."

**Verification commands:**

- `GCX_AGENT_MODE=false make reference` (regen CLI docs, must not drift
  after commit).
- `GCX_AGENT_MODE=false make lint`.
- `GCX_AGENT_MODE=false make tests` (full suite, race enabled).
- `GCX_AGENT_MODE=false make docs` (build MkDocs site).
- `GCX_AGENT_MODE=false make all` (umbrella: lint + tests + build + docs).

**Acceptance criteria:**

- `make reference-drift` passes (the regen step produced no uncommitted
  changes).
- `make lint` exits 0.
- `make tests` exits 0.
- `make docs` builds successfully.
- `make all` exits 0.
- `cmd/gcx/setup/instrumentation/status_test.go` still passes unmodified.
- `CLAUDE.md`'s package-map section lists `internal/setup/framework/` (if
  gate #4 from `docs/reference/doc-maintenance.md` requires it).

**Implementation notes:**

- If `make reference` produces a diff, commit the regenerated files on the
  integration branch (small commit distinct from the implementation task
  commits).
- If a previously passing test now fails because the cmd help tree changed,
  fix the test to match the new help output (don't mute the failure).
