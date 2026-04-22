---
type: feature-plan
title: "Setup Framework: Interfaces, Stubs, and Orchestration"
status: approved
spec: docs/specs/feature-setup-framework/spec.md
created: 2026-04-17
---

# Architecture and Design Decisions

## Pipeline Architecture

The setup framework sits between the global provider registry and the CLI layer. It defines the interfaces that providers opt into, a discovery helper that filters `providers.All()` by interface assertion, and two orchestrating commands that consume discovery results. Per-provider `setup` subcommands reuse the exact same `Setup()` method the orchestrator calls, so there is one implementation behind two entry points.

```
providers.All()  (internal/providers.Registry)
        │
        ▼
internal/setup/framework/                              (NEW)
    ├─ interfaces.go      StatusDetectable / Setupable
    │                     ProductStatus / ProductState / ParamKind
    │                     InfraCategory / InfraCategoryID / SetupParam
    │                     ErrSetupNotSupported
    ├─ discovery.go       DiscoverStatusDetectable() / DiscoverSetupable()
    │                     (type assertions over providers.All())
    ├─ aggregate.go       AggregateStatus(ctx, timeout)
    │                     errgroup (bounded=10) + per-provider
    │                     context.WithTimeout(5s) + error isolation
    │                     + alphabetical sort by ProductName()
    ├─ stub.go            ConfigKeysStatus(p providers.Provider)
    │                     shared helper: not_configured / configured
    │                     based on ConfigKeys() presence in current ctx
    ├─ orchestrator.go    Run(ctx, Options)
    │                     discovery → category select → skip-if-configured
    │                     → param collect → ValidateSetup (retry loop)
    │                     → preview (masked) → sequential Setup
    │                     → summary; signal.NotifyContext(os.Interrupt)
    └─ prompt/            Text / Bool / Choice / MultiChoice / Secret
                          widgets (bufio + golang.org/x/term)
        │                                  ▲
        ▼                                  │
CLI layer (cmd/gcx/setup/)                 │
    ├─ command.go   `gcx setup status`     │
    │               → framework.Aggregate  │
    │               → internal/output      │
    │                   (text/json/yaml/   │
    │                    wide) + style     │
    │                    TableBuilder      │
    │                                      │
    ├─ run.go       `gcx setup run`        │
    │   (NEW)       → agent.IsAgentMode()  │
    │               → framework.Run        │
    │                   └─ uses prompt/ ───┘
    │
    └─ instrumentation/  (UNCHANGED subtree — coexists)
        └─ gcx setup instrumentation {status|show|apply|discover|export}

Per-provider entry points (also wired through the same Setup()):
    internal/providers/<area>/setup.go  (NEW for 9 setup-capable)
        → registered via provider.Commands()
        → `gcx <provider-area> setup`    (thin Cobra wrapper over
                                          Setupable.Setup())

Provider stubs (14 files touched):
    Signal providers (StatusDetectable only):
        alert, logs, metrics, profiles, traces
            └─ Status() delegates to framework.ConfigKeysStatus()
    Setup-capable providers (Setupable, stubs):
        appo11y, faro, fleet, irm, k6, kg, sigil, slo, synth
            ├─ Status() delegates to framework.ConfigKeysStatus()
            ├─ InfraCategories(), ResolveChoices(), ValidateSetup() stubs
            └─ Setup() returns ErrSetupNotSupported
```

Key flow properties:

- `gcx setup status` and every per-provider `setup` subcommand share a single downstream function per concern (`AggregateStatus`, `Setup`). Adding a new provider that implements the interfaces automatically surfaces in status output and the run orchestrator — no CLI wiring required beyond the per-provider command.
- Framework is imported by providers; providers are never imported by framework (one-way dependency, cycle-free).
- `cmd/gcx/setup/instrumentation/` is untouched — the legacy subtree keeps working verbatim.

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Framework package lives at `internal/setup/framework/` (new package) | Keeps framework logic out of `internal/providers` to avoid import cycles; mirrors the existing `internal/setup/instrumentation/` naming. An alternative `internal/providers/setup/` was rejected because providers must import the interface definitions — a framework package that providers depend on (one-way) is cleaner. Traces to FR-001..FR-010. |
| `Setupable` embeds `StatusDetectable` at the interface level | Compile-time invariant that every setup-capable provider also exposes status; prevents drift where `setup run` offers a product that has no `status` entry. Traces to FR-002. |
| `ProductState` is a typed string enum (`not_configured`, `configured`, `active`, `error`) | Human-readable in text codec, round-trips cleanly through JSON/YAML codecs without a custom marshaller, stable API surface for agent consumers. Traces to FR-003, FR-004. |
| Status aggregation uses `errgroup.Group` with bounded parallelism = 10 and per-provider `context.WithTimeout(5s)` | Matches the codebase-wide concurrency standard (CLAUDE.md). 5s default answers the spec's [NEEDS CLARIFICATION] open question — Status() probes are expected to be fast (single HTTP call or cached lookup), and 5s matches typical provider HTTP timeouts in gcx. Overridable via a status command flag in a later iteration. Traces to FR-012, FR-013. |
| Error isolation: a `Status()` error is wrapped into `ProductStatus{State: error, Details: err.Error()}` and rendered inline | One slow/broken provider must not collapse the whole table. Users see which providers failed and why, alphabetical ordering is preserved. Traces to FR-014, FR-015. |
| Alphabetical ordering via `slices.SortFunc` on `ProductName()` after aggregation, before render | Deterministic output for snapshot tests and agent consumers; simpler than maintaining sort order in the aggregator goroutine channel. Traces to FR-016. |
| Codec wiring reuses `internal/output.Options`, registering a custom text codec via `RegisterCustomCodec` for the status table | Matches Pattern 13 (format-agnostic fetching) — aggregation does not care about output format; codecs control rendering only. JSON/YAML codecs map `[]ProductStatus` directly through the default encoders. Traces to FR-017, FR-019, FR-020. |
| Text-codec table rendering uses `internal/style.TableBuilder` and the Grafana Neon Dark palette for state colouring | Consistency with the rest of gcx; color is only applied when stdout is a TTY (existing `internal/terminal.Detect` gating). Traces to FR-018. |
| Agent-mode default flip to JSON on `gcx setup status` reuses `agent.IsAgentMode()` inside the output options default resolver | Same mechanism every other gcx command uses; no new detection path. Traces to FR-019, FR-020. |
| `gcx setup run` refuses in agent mode via an early `agent.IsAgentMode()` check in `RunE` returning a usage error with exit code 2 | Interactive orchestration cannot function in agent mode; failing fast with an actionable error preserves the agent-safety contract from CONSTITUTION/DESIGN. Traces to FR-036. |
| `InfraCategory` conflicts: first write wins, keyed by `InfraCategoryID` in a `map[InfraCategoryID]InfraCategory` seeded in alphabetical-provider order | Deterministic merge, cheap to compute, and the alphabetical seeding means the category owner is predictable from the provider order. Future spec can elevate this to an explicit precedence registry if conflicts emerge in practice. Traces to FR-021. |
| Skip-if-configured check runs *before* param collection, not before preview | Avoids prompting for params the user will not need if the provider reports `configured` or `active`. Traces to FR-027. |
| Validation retry loop: on `ValidateSetup` error, re-prompt every param for the current provider with previously-collected values as defaults; loop until validation succeeds or the user cancels | Conforms to DESIGN's "fail fast with actionable errors" while letting users correct typos without aborting the whole flow; avoids parsing validator error strings to target specific fields. Traces to FR-029. |
| Preview step masks every param where `Secret=true` using `***` before display | Prevents accidental token disclosure in terminal scrollback and screen-shares. Traces to FR-023, FR-030. |
| Sequential `Setup()` invocation (not parallel) in the orchestrator | Order matters for cross-provider dependencies (e.g. instrumentation depends on infra category choices); sequential is also easier to reason about for a first version. Traces to FR-032, FR-037. |
| Ctrl-C handling via `signal.NotifyContext(ctx, os.Interrupt)` wrapping the run loop; the orchestrator catches `ctx.Err() == context.Canceled` and renders the summary of completed/skipped/failed products | Clean shutdown without orphaned prompts; summary preserves audit trail. Traces to FR-034. |
| Per-provider `gcx <provider-area> setup` command is a thin Cobra wrapper that calls the same `Setupable.Setup()` the orchestrator uses | One implementation, two entry points; the wrapper uses the opts pattern (opts struct + `setup(flags)` + `Validate()` + constructor) and is flag-only (no prompts) so it is scriptable and agent-friendly. Traces to FR-039, FR-040, FR-041, FR-043, FR-044, FR-045. |
| Stub implementation strategy: a shared `framework.ConfigKeysStatus(p providers.Provider) ProductStatus` helper inspects `p.ConfigKeys()` in the current context — returns `configured` if all required keys have non-empty values, else `not_configured` | Lets all 14 providers ship a working `Status()` today without implementing product-specific probes; product-specific `active` detection lands per-provider later. Traces to FR-046, FR-047. |
| Setup-capable stubs implement `InfraCategories() []InfraCategory` returning `nil`, `ResolveChoices(...)` returning `(nil, nil)`, `ValidateSetup(...)` returning `nil`, and `Setup(...)` returning `ErrSetupNotSupported` | Framework code treats `ErrSetupNotSupported` as a non-failure sentinel in the orchestrator summary (marked "not yet implemented") but as a hard error when invoked directly via the per-provider command (non-zero exit). Traces to FR-048..FR-053. |
| Interactive prompt widgets are an in-repo minimal implementation using `bufio.Reader` + `golang.org/x/term` (`term.MakeRaw` for raw input, `term.ReadPassword` for secret masking) | `x/term` is already transitively available via Cobra's dependency graph, keeping the dependency surface small. Avoids heavy TUI libraries (bubbletea, survey/v2) whose scope far exceeds what the orchestrator needs. Traces to FR-023, FR-028. |
| Prompt widgets preserve defaults: pressing Enter on a prompt with `Default` accepts it; `Required` params without `Default` re-prompt | Keeps happy-path short; explicit `Required` semantics are enforced at the widget level, not deferred to `ValidateSetup`. Traces to FR-028. |
| Test strategy: interface-level table-driven tests using fake providers (implementing `StatusDetectable`/`Setupable`) live in an internal test package; integration tests at the command level use a registry override helper (`SetupTestRegistry`) that swaps `providers.All()`'s backing slice | Decouples framework tests from the real provider set; keeps test runs deterministic and network-free. Traces to spec Test Strategy. |
| Backward-compat guard: `cmd/gcx/setup/instrumentation/` subtree is not touched — only `cmd/gcx/setup/command.go`'s `status` command body is rewritten to call `framework.AggregateStatus` | Keeps the Instrumentation migration path stable; the legacy `status_test.go` must continue to pass unchanged as a regression guard. Traces to FR-054. |

## Compatibility

**What continues working unchanged:**

- `gcx setup instrumentation` subtree in full: `status`, `show`, `apply`, `discover`, `export`. None of those files are edited.
- All existing provider `Commands()` return values — adding stub interfaces does not remove or rename any existing subcommand.
- The base `providers.Provider` interface contract (Name, ShortDesc, Commands, Validate, ConfigKeys, TypedRegistrations).
- Agent-mode auto-detection (`internal/agent`) and every codec in `internal/output`.
- `cmd/gcx/setup/instrumentation/status_test.go` passes without modification (regression guard).

**What is deprecated:**

- The old hardcoded shape of `gcx setup status` output. Previously this command rendered an instrumentation-only table; it now renders an aggregated all-providers table. Callers parsing the old shape must migrate by either:
  1. Switching to `--output json` and consuming structured `ProductStatus` records, or
  2. Calling `gcx setup instrumentation status` for the narrower instrumentation-only view.

No config keys, env vars, or flags are removed. The command name itself is unchanged.

**What is newly available:**

- Public Go types in `internal/setup/framework`: `StatusDetectable`, `Setupable`, `ProductStatus`, `ProductState`, `InfraCategory`, `InfraCategoryID`, `SetupParam`, `ParamKind`, `ErrSetupNotSupported`.
- New CLI commands: `gcx setup run` (interactive orchestrator) and `gcx <provider-area> setup` for each of the 9 setup-capable providers (appo11y, faro, fleet, irm, k6, kg, sigil, slo, synth). All 9 per-provider commands currently return `ErrSetupNotSupported`; Area 7 of the roadmap will replace the stubs with real implementations.
- New `gcx setup status` rendering across all 14 providers with codec support (text / json / yaml / wide) and agent-mode JSON default.

## Package Layout

New and touched paths:

- `internal/setup/framework/` **(new package)**
  - `interfaces.go` — `StatusDetectable`, `Setupable`, `InfraCategory`, `InfraCategoryID`, `SetupParam`, `ProductStatus`, `ProductState`, `ParamKind`, `ErrSetupNotSupported`, `ValidationError`.
  - `discovery.go` — `DiscoverStatusDetectable()`, `DiscoverSetupable()` — type-assertion helpers over `providers.All()`.
  - `aggregate.go` — `AggregateStatus(ctx context.Context, timeout time.Duration) []ProductStatus` — parallel aggregation (errgroup, bounded=10), per-provider `context.WithTimeout`, error isolation via `ProductStatus{State: error}` synthesis, alphabetical sort.
  - `orchestrator.go` — `Run(ctx context.Context, opts Options) (Summary, error)` — full run flow: discovery, category selection, skip-if-configured, param collection, validation retry, masked preview, sequential `Setup()`, summary. Owns the `signal.NotifyContext(ctx, os.Interrupt)` wrapper.
  - `stub.go` — `ConfigKeysStatus(p providers.Provider) ProductStatus` — shared helper returning `configured` or `not_configured` based on `ConfigKeys()` presence in the current context.
  - `prompt/` — minimal interactive widgets:
    - `prompt.go` — `Text`, `Bool`, `Choice`, `MultiChoice`, `Secret` (uses `golang.org/x/term`).
    - `prompt_test.go`.
  - `*_test.go` — table-driven unit tests using fake providers.
  - `fakes_test.go` (or `internal/fakes`) — reusable `FakeStatusDetectable` / `FakeSetupable` test doubles.

- `cmd/gcx/setup/command.go` **(modified)** — the `status` subcommand body is rewritten to call `framework.AggregateStatus` and render via `internal/output.Options`; the `run` subcommand is added (delegating to `cmd/gcx/setup/run.go`).
- `cmd/gcx/setup/run.go` **(new)** — Cobra constructor for `gcx setup run` using the opts pattern (opts struct + `setup(flags)` + `Validate()` + constructor); early agent-mode refusal; delegation to `framework.Run`.
- `cmd/gcx/setup/command_test.go` **(new or extended)** — integration tests using `framework.SetupTestRegistry`.

- `internal/providers/<area>/provider.go` **(modified — 14 files)** — add `Status()` method to signal providers (5); add `Status()`, `InfraCategories()`, `ResolveChoices()`, `ValidateSetup()`, `Setup()` stubs to setup-capable providers (9).
  - Signal providers (StatusDetectable only): `alert`, `logs`, `metrics`, `profiles`, `traces`.
  - Setup-capable providers (Setupable stub): `appo11y`, `faro`, `fleet`, `irm`, `k6`, `kg`, `sigil`, `slo`, `synth`.
- `internal/providers/<area>/setup.go` **(new — 9 files)** — per-provider Cobra `setup` subcommand; registered via `provider.Commands()`; calls `Setupable.Setup()` which returns `ErrSetupNotSupported` until Area 7 replaces the stubs. Help text marks the command "not yet implemented".
- `internal/providers/<area>/provider_test.go` **(modified — 14 files)** — assert stub method presence and `Setup()` returns `ErrSetupNotSupported` where applicable.

## Implementation Sequence

Ordered workstreams suitable for downstream task decomposition:

1. Scaffold `internal/setup/framework/` with `interfaces.go`, `discovery.go`, `stub.go`, and their table-driven unit tests. Establish the fake-provider test doubles used by later workstreams.
2. Implement `framework.AggregateStatus` (parallel via errgroup with bounded=10, per-provider `context.WithTimeout(5s)`, error isolation, alphabetical sort) with fake-provider table-driven tests covering: happy path, timeout, provider panic, ordering, mixed states.
3. Rewrite `gcx setup status` in `cmd/gcx/setup/command.go` to call `AggregateStatus`; wire `internal/output.Options` with a registered text codec using `internal/style.TableBuilder` and Neon Dark colour mapping. Add integration tests with `framework.SetupTestRegistry`. Verify `cmd/gcx/setup/instrumentation/status_test.go` continues to pass untouched.
4. Implement minimal prompt widgets in `internal/setup/framework/prompt/` (text, bool, choice, multi_choice, secret) with tests asserting masking behaviour and default-preservation on empty input. Use `golang.org/x/term` for secret masking.
5. Implement `framework.Orchestrator` (`Run`) covering: discovery, category select, skip-if-configured (via pre-run `Status()`), param collection, validation retry loop, preview with secret masking, sequential `Setup()` invocation, summary rendering, `signal.NotifyContext` Ctrl-C handling. Fake-provider tests exercise each path including interrupt and validation retry.
6. Add `gcx setup run` command in `cmd/gcx/setup/run.go` using the opts pattern; early agent-mode refusal returning usage-error exit code; delegate to `framework.Run`. Integration tests via `SetupTestRegistry` cover agent-mode refusal and a full interactive flow against a fake provider.
7. Add StatusDetectable-only stubs to the 5 signal providers (`alert`, `logs`, `metrics`, `profiles`, `traces`) by delegating `Status()` to `framework.ConfigKeysStatus`. Per-provider tests assert stub method presence and config-keys-driven state resolution.
8. Add `Setupable` stubs plus `<provider-area> setup` Cobra commands to the 9 setup-capable providers (`appo11y`, `faro`, `fleet`, `irm`, `k6`, `kg`, `sigil`, `slo`, `synth`). Each stub `Setup()` returns `ErrSetupNotSupported`; the Cobra command surfaces a "not yet implemented" help string and exits non-zero when invoked. Per-provider tests assert command existence, exit behaviour, and error sentinel identity.
9. Regenerate reference docs (`GCX_AGENT_MODE=false make reference`) and run `GCX_AGENT_MODE=false make all`.

## Testing Strategy

- **Unit tests** — table-driven for every file under `internal/setup/framework/*.go` and `internal/setup/framework/prompt/*.go`. No network calls; all provider interactions go through fake doubles.
- **Fake providers** — `FakeStatusDetectable` and `FakeSetupable` implemented in the framework test package. Cover configurable state, configurable errors, configurable latencies for timeout tests, and configurable panic for error-isolation tests.
- **Integration tests** — at the command level for `gcx setup status` and `gcx setup run` using a `framework.SetupTestRegistry(providers []providers.Provider) (restore func())` helper that swaps the backing slice of `providers.All()` inside a test (with cleanup restoring the real registry). Runs against Cobra's in-memory `rootCmd.SetOut/SetErr` so stdout is captured.
- **Regression guard** — `cmd/gcx/setup/instrumentation/status_test.go` must continue to pass unmodified.
- **Agent-mode tests** — cover both the default JSON flip on `gcx setup status` and the refusal on `gcx setup run` (exit code 2, usage error message).
- **Codec tests** — assert JSON and YAML outputs round-trip `ProductStatus` records; assert text codec produces a `TableBuilder`-rendered table with deterministic column ordering.
- **Ordering tests** — assert alphabetical-by-ProductName across a mixed set of providers including error rows.
- **Stub tests** — per-provider assertions that signal providers implement `StatusDetectable` and setup-capable providers implement `Setupable`; `Setup()` returns `ErrSetupNotSupported` identity (`errors.Is`).
- **No network calls** in any unit or integration test. Provider HTTP clients are not invoked — all state flows through fakes or `ConfigKeysStatus` which reads in-memory config.

## Risks and Mitigations (Plan-level)

Spec-level risks are enumerated in `spec.md`. The following are plan-specific additions:

| Risk | Impact | Mitigation |
|------|--------|------------|
| Import cycle between `internal/setup/framework` and `internal/providers` if the framework needs to call back into provider types. | Build breaks; framework cannot be imported. | Strict one-way dependency: providers import framework (for interface types), framework imports only the base `providers.Provider` contract via a small read-only accessor if needed. Early Task 1 scaffolding includes a build-time cycle check. |
| Test harness complexity when overriding `providers.All()` for integration tests (global registry is hostile to parallel tests). | Flaky or serialized integration tests; difficult to add new fakes. | Ship `framework.SetupTestRegistry(providers)` returning a `restore func()`. Guard with `t.Cleanup`; document in a `doc.go` that integration tests using it must run with `t.Parallel()` off unless each subtest scopes its own registry. |
| `--output` flag collision with the legacy `gcx setup status` command's existing flag set (if any non-codec flags are present). | Invocation ambiguity, surprising behaviour for scripted callers. | Adopt the standard `internal/output.Options` registration which is the same pattern every other gcx command uses. Inventory existing flags on the `status` command during Task 3 and migrate any conflicting ones before wiring the codec. |
| Prompt widget portability across terminal emulators (raw-mode, non-TTY stdin). | Orchestrator hangs or misrenders on CI/non-TTY stdin despite agent-mode refusal. | Detect non-TTY via `internal/terminal.IsPiped` at the start of `Run`; refuse with an actionable error when stdin is not a TTY. Add a dedicated non-TTY test using a `bytes.Buffer` stdin. |
| Per-provider `setup` command discoverability in the global help tree. | Users unaware that `gcx <provider-area> setup` exists. | Ensure each per-provider command contributes a `setup` entry to `cmd/gcx/helptree/`; update the agent metadata catalog in `cmd/gcx/commands/` as part of Task 8. |
| Ctrl-C during a secret prompt leaves the terminal in raw mode. | Broken user terminal post-abort. | `prompt.Secret` registers a `defer term.Restore(fd, state)` immediately after `term.MakeRaw`; unit-test the restore path by forcing a panic inside the read call. |
