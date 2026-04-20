---
type: feature-spec
title: "Setup Framework: Interfaces, Stubs, and Orchestration"
status: done
adr: docs/adrs/setup-framework/001-setup-framework-interfaces-and-orchestration.md
created: 2026-04-17
---

# Setup Framework: Interfaces, Stubs, and Orchestration

## Problem Statement

gcx today ships 14 providers plus the instrumentation subsystem (15 setup-capable participants in total) but has no unified way to answer "what is configured, what is broken, and what should I do next?" Each provider's onboarding path is independent — there is no orchestrated flow from "just authenticated" to "products working."

The current `gcx setup status` (cmd/gcx/setup/command.go:55-92) is hardcoded to instrumentation: it calls `instrum.NewClient(r.Client).RunK8sMonitoring` and renders a fixed PRODUCT|ENABLED|HEALTH|DETAILS table. It cannot surface state for any other provider. There is no `gcx setup run` command today, and there is no contract for a per-provider `gcx <provider-area> setup` command either.

Users (and agents) who just logged in must discover, enable, and validate products one at a time by reading per-provider docs and inventing their own sequencing. This is a growing onboarding gap as the provider count increases. The approved ADR (docs/adrs/setup-framework/001-setup-framework-interfaces-and-orchestration.md) locks in a three-entry-point model that closes this gap:

- `gcx setup status` — aggregated product status dashboard across every provider.
- `gcx setup run` — interactive, re-runnable orchestrator that skips already-configured products.
- `gcx <provider-area> setup` — non-interactive per-provider setup commands, agent-friendly.

This spec turns the ADR into an executable plan: the interfaces, types, orchestration rules, stub coverage, and tests that must exist for the framework to ship. Rich per-provider logic (real API probes in `Status()`, working `Setup()` flows, populated `InfraCategories()`) is deliberately deferred to Area 7 and is out of scope here.

## Scope

### In Scope

- Two optional interfaces: `StatusDetectable` and `Setupable`, with `Setupable` embedding `StatusDetectable`.
- Supporting types: `ProductStatus`, `ProductState` (enum), `InfraCategory`, `InfraCategoryID`, `SetupParam`, `ParamKind`.
- A sentinel error `ErrSetupNotSupported` for setup-capable providers whose stub `Setup()` is not yet implemented.
- Capability discovery via Go type assertion on `providers.All()` — no hard-coded provider lists anywhere.
- `gcx setup status` rewritten to aggregate every `StatusDetectable` provider (parallel calls with per-provider timeout, deterministic alphabetical rendering, error isolation).
- `gcx setup status` rendered via the standard codec system (`text`, `json`, `yaml`, `wide`) with human color-coding and agent-mode color suppression.
- `gcx setup run` command implementing the ADR §6 flow: discovery → category multi-select → provider resolution → per-provider loop (skip-if-configured → parameter collection → validation retry) → preview → sequential `Setup()` execution → post-run status summary.
- Ctrl-C handling in `gcx setup run`: print summary of completed vs remaining providers, exit with code 5 (cancelled). No rollback.
- Agent-mode refusal of `gcx setup run` (exit code 2 usage error) with a message pointing to per-provider setup commands.
- Per-provider `gcx <provider-area> setup` Cobra command contract: opts pattern, flag-only, idempotent, agent-friendly, codec-rendered mutation summary on success, non-zero exit on failure.
- Orchestrator invocation of `Setupable.Setup()` in-process only — never subprocess.
- Stub coverage across all 14 existing providers:
  - **Signal/data providers** (alert, logs, metrics, profiles, traces) implement `StatusDetectable` only. `Status()` uses a `ConfigKeys()` presence heuristic; no API probing.
  - **Setup-capable providers** (appo11y, faro, fleet, incidents, k6, kg, sigil, slo, synth) implement `Setupable`. `Setup()` returns `ErrSetupNotSupported`. `InfraCategories()` returns `nil`. `ValidateSetup()` returns `nil` (no-op). `ResolveChoices()` returns `(nil, nil)`.
- Interactive prompt widgets for `text`, `bool`, `choice` (single select), `multi_choice` (multi-select), and secret (masked input). Minimal implementation, no heavy UI dependency.
- Preview rendering with secret masking (`***` for any `SetupParam` where `Secret == true`).
- Validation retry flow: on `ValidateSetup` error, re-prompt all fields for that provider with previously-collected values as defaults; loop until validation passes or the user cancels.
- Backward compatibility: the existing `gcx setup instrumentation {status|show|apply|discover|export}` subtree continues to work unchanged.
- Test coverage: table-driven unit tests for status aggregation, ordering, error isolation, codec rendering, stub behavior, per-provider setup command idempotency, validation retry, agent-mode refusal.

### Out of Scope

- **Rich per-provider `Status()` probes** (real API calls, resource counting, token-expiry checks). Deferred to Area 7 — this spec ships stubs.
- **Real `Setup()` flows** for the 9 setup-capable providers. All stubs return `ErrSetupNotSupported`. Area 7 replaces them one provider at a time.
- **Populated `InfraCategories()`** on any provider. All stubs return `nil`. Area 7 adds them per provider.
- **`ConcurrencySafe()` method or `--parallel` flag** on `gcx setup run`. `Setup()` execution is sequential in v1 (ADR §6, rejected alternative "Concurrent `Setup()` execution").
- **Dependency-aware provider ordering** (e.g., instrumentation before metrics). Alphabetical only in v1.
- **Field-level validation error targeting.** Validation failure re-prompts all fields for the provider; no structured error protocol between `ValidateSetup` and the orchestrator.
- **`--dry-run` / `--show-commands` preview modes** on `gcx setup run`. Deferred.
- **Rich choice metadata** (`[]Choice{Value, Description}`). `ResolveChoices` returns `[]string` only.
- **`gcx setup doctor`** deep health checks (token expiry, API version compatibility). Future work listed in ADR §Follow-up.
- **Changes to the base `Provider` interface** in internal/providers/provider.go. The new interfaces are additive and optional.
- **Subprocess-based orchestration.** Never.
- **Rollback of completed `Setup()` calls** on error or cancellation. Partial completion is a valid state.

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Interface shape | Two optional interfaces: `StatusDetectable` (2 methods) and `Setupable` embedding `StatusDetectable` (+4 methods) | Status-only providers stay minimal; embedding enforces at compile time that anything in `setup run` is also in the status table | ADR-001 §1 |
| Status payload | `ProductStatus{Product, State, Details, SetupHint}` with `ProductState` enum (`not_configured`, `configured`, `active`, `error`) | Flat, codec-friendly shape; enum values are machine-readable and color-map cleanly in human mode | ADR-001 §2 |
| Status aggregation contract | Parallel `Status()` calls with per-provider timeout; errors surfaced as `StateError` rows; deterministic alphabetical ordering; standard codec system | Error isolation prevents one slow/broken provider from blocking the table; alphabetical ordering makes output diffable and stable | ADR-001 §3 |
| Category + param model | `InfraCategory{ID, Label, Params}` multi-select (first label wins on ID collision); `SetupParam{Name, Prompt, Kind, Required, Default, Secret, Choices}` with `ParamKind` enum | Declarative schema drives prompt widget, flag registration, and preview rendering from one source | ADR-001 §4 |
| Per-provider setup command contract | Every `Setupable` exposes `gcx <provider-area> setup` as a Cobra subcommand using the opts pattern; non-interactive, flag-only, idempotent, agent-friendly, codec-rendered output | Matches existing gcx UX rules; agents get a stable non-interactive entry; one implementation (`Setupable.Setup()`) powers both CLI and orchestrator | ADR-001 §5 |
| Orchestration model | `gcx setup run` discovers categories, multi-selects, resolves providers alphabetically, skips already-configured, collects params, validates with full-param retry, previews with secret masking, executes sequentially with stderr progress, renders status summary; Ctrl-C prints summary and exits | Sequential `Setup()` keeps progress legible, error attribution clear, preserves instrumentation→signal ordering, matches small-N reality | ADR-001 §6 |
| Stub coverage | All 14 existing providers get stubs on day one: signals implement only `StatusDetectable` (config-key presence heuristic); setup-capable providers implement `Setupable` with `Setup()` returning `ErrSetupNotSupported` and `InfraCategories()` returning `nil` | Validates the interfaces end-to-end, populates the status table from day one, lets Area 7 land rich implementations provider-by-provider without framework churn | ADR-001 §7 |

## Functional Requirements

**Interfaces and types**

- FR-001: The system MUST define a `StatusDetectable` interface with two methods: `ProductName() string` and `Status(ctx context.Context) (*ProductStatus, error)`.
- FR-002: The system MUST define a `Setupable` interface that embeds `StatusDetectable` and adds `ValidateSetup(ctx, params map[string]string) error`, `Setup(ctx, params map[string]string) error`, `InfraCategories() []InfraCategory`, and `ResolveChoices(ctx, paramName string) ([]string, error)`.
- FR-003: The system MUST define a `ProductState` string enum with exactly four values: `not_configured`, `configured`, `active`, `error`.
- FR-004: The system MUST define a `ProductStatus` struct with fields `Product string`, `State ProductState`, `Details string`, `SetupHint string`.
- FR-005: The system MUST define an `InfraCategory` struct with fields `ID InfraCategoryID`, `Label string`, `Params []SetupParam`, and an `InfraCategoryID` string alias.
- FR-006: The system MUST define a `ParamKind` string enum with exactly four values: `text`, `bool`, `choice`, `multi_choice`.
- FR-007: The system MUST define a `SetupParam` struct with fields `Name`, `Prompt`, `Kind`, `Required`, `Default`, `Secret`, `Choices`.
- FR-008: The system MUST export a sentinel error `ErrSetupNotSupported` that setup-capable stubs return from `Setup()` to signal the flow is not yet implemented.
- FR-009: The system MUST NOT extend the base `Provider` interface (internal/providers/provider.go) — the new interfaces are strictly additive and optional.

**Discovery**

- FR-010: The system MUST discover setup capabilities by iterating `providers.All()` and applying Go type assertions for `StatusDetectable` and `Setupable`. It MUST NOT reference provider package names, provider lists, or any registry outside of `providers.All()`.
- FR-011: Providers that implement neither interface MUST remain fully functional as regular providers and MUST be invisible to the setup framework.

**`gcx setup status` aggregation**

- FR-012: The `gcx setup status` command MUST call `Status()` on every `StatusDetectable` provider in parallel using `errgroup` with bounded parallelism (default 10, matching the codebase standard).
- FR-013: Each `Status()` call MUST run under a per-provider timeout enforced by the orchestrator via `context.WithTimeout`. The default timeout MUST be 5 seconds (matching the typical HTTP call budget in gcx; see plan.md Design Decisions).
- FR-014: An error returned by `Status()` MUST be rendered as a row with `State = error` (StateError) and the error message placed in `Details`. The error MUST NOT cancel sibling `Status()` calls.
- FR-015: A `Status()` call that exceeds its timeout MUST be treated as an error per FR-014 (row with `State = error`, message indicating timeout), not a hang, not a cancellation of other providers.
- FR-016: Status rows MUST be rendered in alphabetical order by `ProductName()`, independent of call completion order.
- FR-017: Status output MUST be rendered through the standard codec system (internal/output) supporting at least `text`, `json`, `yaml`, and `wide` formats. The command MUST NOT print bespoke formats or bypass the codec registry.
- FR-018: In human mode, the `text` codec MUST color-code `State` (gray=not_configured, yellow=configured, green=active, red=error) using internal/style.
- FR-019: In agent mode (`agent.IsAgentMode() == true`), the status command MUST default `--output` to `json`, suppress color, and suppress truncation.
- FR-020: `gcx setup status` MUST work in agent mode.

**`InfraCategory` and `SetupParam` semantics**

- FR-021: When multiple providers register the same `InfraCategoryID`, the orchestrator MUST use the label of the first provider encountered (in alphabetical order of `ProductName()`) and ignore subsequent labels for that ID.
- FR-022: `SetupParam` with `Kind ∈ {choice, multi_choice}` MUST populate options from `Choices` when non-empty. When `Choices` is empty, the orchestrator MUST call `ResolveChoices(ctx, param.Name)` to obtain options.
- FR-023: `SetupParam.Secret == true` MUST cause the interactive prompt to mask input and MUST cause the preview step to render the value as `***`.

**`gcx setup run` orchestration**

- FR-024: `gcx setup run` MUST discover categories by iterating `providers.All()`, type-asserting `Setupable`, and collecting every non-nil `InfraCategories()` result.
- FR-025: `gcx setup run` MUST present a multi-select of discovered category labels and allow the user to pick zero or more.
- FR-026: After category selection, `gcx setup run` MUST resolve the set of providers whose `InfraCategories()` contains any selected ID and process them sequentially in alphabetical order of `ProductName()`.
- FR-027: Before collecting parameters for a provider, the orchestrator MUST call `Status(ctx)`. If the result is `StateConfigured` or `StateActive`, the orchestrator MUST print "already configured, skipping" to stderr and continue to the next provider.
- FR-028: For each `SetupParam`, the orchestrator MUST render the prompt widget matching `Kind`: plain text input for `text`, yes/no for `bool`, single-select list for `choice`, multi-select list for `multi_choice`. `Secret == true` MUST mask the text input.
- FR-029: After parameter collection for a provider, the orchestrator MUST call `ValidateSetup(ctx, params)`. On error, it MUST print the error to stderr and re-run parameter collection for that provider with previously-collected values pre-filled as defaults. The loop MUST continue until validation succeeds or the user cancels.
- FR-030: Before any `Setup()` call, the orchestrator MUST render a preview block listing each selected provider and its parameters. Providers with no parameters MUST render with their name only. Parameters where `Secret == true` MUST render as `***`.
- FR-031: The orchestrator MUST prompt for confirmation (`Continue? [Y/n]`) after the preview. A negative answer MUST exit without invoking any `Setup()` and MUST use exit code 5 (cancelled).
- FR-032: After confirmation, the orchestrator MUST call `Setup(ctx, params)` for each confirmed provider sequentially in alphabetical order of `ProductName()`, streaming per-provider progress to stderr.
- FR-033: If `Setup()` returns an error, the orchestrator MUST log the error to stderr and continue to the next provider. It MUST NOT abort the run and MUST NOT roll back any prior successful `Setup()` call.
- FR-034: On Ctrl-C (SIGINT) at any point during the run, the orchestrator MUST print a summary to stderr listing completed providers and remaining providers, then exit with code 5 (cancelled). It MUST NOT attempt rollback.
- FR-035: After the execution phase, the orchestrator MUST render the post-run status table using the same aggregator as `gcx setup status`.
- FR-036: In agent mode (`agent.IsAgentMode() == true`), `gcx setup run` MUST refuse to execute, MUST print a message to stderr directing the user to per-provider `gcx <provider-area> setup` commands, and MUST exit with code 2 (usage error).
- FR-037: `gcx setup run` MUST NOT call `Setup()` concurrently. All `Setup()` invocations in the run orchestrator are sequential.
- FR-038: The orchestrator MUST invoke `Setupable.Setup()` as a direct in-process method call. It MUST NOT invoke `gcx <provider-area> setup` as a subprocess.

**Per-provider `gcx <provider-area> setup` command**

- FR-039: Every provider that implements `Setupable` MUST expose a Cobra subcommand `gcx <provider-area> setup` that is a thin wrapper over `Setupable.Setup()`.
- FR-040: The per-provider setup command MUST follow the codebase opts pattern: `opts struct` + `setup(flags *pflag.FlagSet)` + `Validate() error` + constructor returning `*cobra.Command`.
- FR-041: The per-provider setup command MUST be non-interactive: flags only, no stdin prompts. It MUST work with `stdin` closed.
- FR-042: The per-provider setup command MUST be idempotent: re-running it against an already-configured product MUST NOT produce a duplicate configuration or a spurious error; it either no-ops or updates in place.
- FR-043: On success, the per-provider setup command MUST emit a structured mutation summary through the standard codec system (`text|json|yaml|wide`) and exit with code 0.
- FR-044: On failure, the per-provider setup command MUST write a diagnostic to stderr, MUST NOT emit a partial data payload to stdout, and MUST exit with a non-zero exit code per DESIGN.md (1 general, 2 usage, 3 auth, 6 version-incompatible as appropriate).
- FR-045: The per-provider setup command MUST work in agent mode with no prompts and JSON output by default.

**Stub implementations**

- FR-046: Every existing signal provider (alert, logs, metrics, profiles, traces) MUST implement `StatusDetectable` and MUST NOT implement `Setupable`.
- FR-047: Every existing signal provider stub `Status()` MUST determine state from `ConfigKeys()` presence: when every non-secret required config key has a value, return `StateConfigured`; otherwise return `StateNotConfigured`. The stub MUST NOT perform any API probe.
- FR-048: Every existing setup-capable provider (appo11y, faro, fleet, incidents, k6, kg, sigil, slo, synth) MUST implement `Setupable`.
- FR-049: Every existing setup-capable provider stub `Setup()` MUST return `ErrSetupNotSupported`.
- FR-050: Every existing setup-capable provider stub `InfraCategories()` MUST return `nil`.
- FR-051: Every existing setup-capable provider stub `ValidateSetup()` MUST return `nil` (no-op validation).
- FR-052: Every existing setup-capable provider stub `ResolveChoices()` MUST return `(nil, nil)`.
- FR-053: Every `Setupable` stub MUST still expose a `gcx <provider-area> setup` Cobra command per FR-039. The command body MAY surface `ErrSetupNotSupported` via non-zero exit until Area 7 replaces the stub.

**Backward compatibility**

- FR-054: The existing subcommand tree `gcx setup instrumentation {status, show, apply, discover, export}` (cmd/gcx/setup/instrumentation/) MUST continue to function identically — command paths, flags, output shapes, exit codes unchanged.
- FR-055: `gcx setup status` output semantics CHANGE: it now aggregates every `StatusDetectable` provider instead of the hardcoded instrumentation-only table. This is the intended behavior and MUST be documented in release notes.

## Acceptance Criteria

**Status aggregation**

- GIVEN the provider registry contains providers A, B, C where A and C implement `StatusDetectable` and B does not
  WHEN the user runs `gcx setup status`
  THEN the rendered table contains rows for A and C only, in alphabetical order, and does not contain a row for B

- GIVEN three `StatusDetectable` providers where provider "Logs" returns `StateActive`, provider "Metrics" returns `StateConfigured`, and provider "Traces" returns `StateNotConfigured`
  WHEN the user runs `gcx setup status --output text`
  THEN the output renders three rows in alphabetical order (Logs, Metrics, Traces) with colorized states (green, yellow, gray respectively)

- GIVEN three `StatusDetectable` providers where provider "Faro" returns an error from `Status()`
  WHEN the user runs `gcx setup status`
  THEN the row for "Faro" renders with `State = error` and the error message in `Details`, the other two provider rows render their real state, and the sibling `Status()` calls are not cancelled

- GIVEN a `StatusDetectable` provider whose `Status()` sleeps past the per-provider timeout
  WHEN the user runs `gcx setup status`
  THEN the row for that provider renders with `State = error` and a timeout message in `Details`, and the command returns within a bounded time proportional to the timeout (not to the sleep)

- GIVEN `Status()` calls complete in the order C, A, B
  WHEN the status table is rendered
  THEN rows appear in alphabetical order A, B, C

**Codec output shapes**

- GIVEN a `gcx setup status --output json` invocation with two `StatusDetectable` providers
  WHEN the command writes to stdout
  THEN stdout contains a JSON array of two objects, each with keys `product`, `state`, `details`, `setup_hint`, and stderr contains no data payload

- GIVEN a `gcx setup status --output yaml` invocation
  WHEN the command writes to stdout
  THEN stdout contains a YAML list with the same fields as the JSON variant and no ANSI color codes

- GIVEN `gcx setup status` invoked while `agent.IsAgentMode()` returns true
  WHEN no `--output` flag is passed
  THEN the command defaults to JSON output, suppresses color, and suppresses truncation

**`gcx setup run` happy path**

- GIVEN three `Setupable` providers registered to the same category "kubernetes" with different parameters, and `Status()` returns `StateNotConfigured` for all three
  WHEN the user runs `gcx setup run`, selects the "kubernetes" category, provides valid parameters for each, and confirms the preview
  THEN `Setup()` is called sequentially in alphabetical order for all three providers with the collected params, and the command exits with code 0 and renders a final status table showing the post-run state

- GIVEN a `Setupable` provider whose `Status()` returns `StateActive`
  WHEN the orchestrator reaches that provider during the per-provider loop
  THEN the orchestrator prints "already configured, skipping" to stderr, does not prompt for parameters, and does not call `Setup()` for that provider

- GIVEN a `Setupable` provider whose first `ValidateSetup(params)` call returns a non-nil error
  WHEN the orchestrator receives the validation error
  THEN it prints the error to stderr and re-prompts every parameter for that provider with the previously-collected values as the default value for each field, and loops until `ValidateSetup` returns nil

- GIVEN a `Setupable` provider with a `SetupParam` where `Secret == true` and the user entered the value "supersecret"
  WHEN the orchestrator renders the preview block
  THEN the parameter's value is rendered as `***` and the raw value "supersecret" does not appear anywhere in stdout or stderr

- GIVEN `gcx setup run` has completed `Setup()` for two providers and the user sends SIGINT before the third provider completes
  WHEN the signal is received
  THEN stderr contains a summary listing the two completed providers and the remaining provider(s), the command exits with code 5, and no rollback of completed providers is attempted

- GIVEN `gcx setup run` is invoked while `agent.IsAgentMode()` returns true
  WHEN the command starts
  THEN it writes a message to stderr pointing the user to per-provider `gcx <provider-area> setup` commands, does not prompt, and exits with code 2

**Per-provider setup command**

- GIVEN a `Setupable` provider "foo" has been configured successfully once
  WHEN the user re-runs `gcx foo setup` with the same flags
  THEN the command detects existing configuration, either no-ops or updates idempotently, emits a structured mutation summary through the codec system, and exits with code 0

- GIVEN `gcx foo setup --output json` is invoked with valid flags in agent mode
  WHEN the command succeeds
  THEN stdout contains only JSON, stderr contains progress/diagnostic text only, and the exit code is 0

- GIVEN `gcx foo setup` is invoked with an invalid flag value
  WHEN the command validates flags via `opts.Validate()`
  THEN the command writes a usage-style diagnostic to stderr, exits with code 2, and does not call `Setup()`

**Stub behavior**

- GIVEN the signal provider "metrics" is `StatusDetectable` only and its required config keys are all present in the active context
  WHEN `gcx setup status` evaluates its row
  THEN the row shows `State = configured` (from `ConfigKeys()` presence heuristic) and `Details` indicates config-key presence only — no API probe has been performed

- GIVEN a setup-capable provider stub "synth" whose `Setup()` returns `ErrSetupNotSupported`
  WHEN the user runs `gcx synth setup --...`
  THEN the command exits with a non-zero code and stderr contains a clear "setup not yet implemented" message

- GIVEN every setup-capable provider stub returns `nil` from `InfraCategories()`
  WHEN the user runs `gcx setup run`
  THEN the category multi-select presents zero categories, the command prints a message indicating no interactive setup flows are available, and exits with code 0

**Backward compatibility**

- GIVEN the framework is shipped
  WHEN the user runs `gcx setup instrumentation status`, `gcx setup instrumentation show`, `gcx setup instrumentation apply`, `gcx setup instrumentation discover`, or `gcx setup instrumentation export`
  THEN each command behaves identically to its pre-framework behavior (flags, output shape, exit codes), and cmd/gcx/setup/instrumentation/status_test.go continues to pass

## Negative Constraints

- NEVER hard-code a list of providers anywhere in the setup framework. Discovery is exclusively via `providers.All()` + type assertion.
- NEVER extend the base `Provider` interface (internal/providers/provider.go) as part of this feature. New interfaces are additive and optional.
- NEVER require a provider to implement `Setupable`. `StatusDetectable`-only is a first-class case.
- NEVER run `Setup()` concurrently in the `gcx setup run` orchestrator. v1 is strictly sequential.
- NEVER cancel sibling `Status()` calls when one provider's `Status()` errors or times out. Error isolation is mandatory.
- NEVER roll back completed `Setup()` calls on cancellation, Ctrl-C, or a later provider's error. Partial completion is a valid state.
- NEVER execute `gcx setup run` in agent mode. The command MUST refuse with exit code 2 and a message directing to per-provider setup commands.
- NEVER invoke a provider's `Setup()` through a subprocess (`exec "gcx" ...`) from the orchestrator. Direct in-process method call only.
- NEVER output result data via printf/fmt or bespoke renderers — every data payload goes through the codec system.
- NEVER mix data and diagnostics on stdout. STDOUT is data, STDERR is progress and diagnostics.
- NEVER emit a secret parameter's raw value to stdout or stderr anywhere in the preview, progress, or summary. Secret values render as `***`.
- NEVER parse `ValidateSetup` error strings to target specific fields. Re-prompt all fields for the provider.
- NEVER break the `gcx setup instrumentation {status|show|apply|discover|export}` subtree. It is compatibility-locked.
- DO NOT introduce a heavy interactive UI library for v1 prompt widgets. Keep widgets minimal and self-contained.
- DO NOT render a status table via a bespoke format. Always via the codec system.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Consistency drift across provider `Status()` and `Setup()` implementations (each provider owns its semantics) | Divergent user experience; inconsistent color/state meanings across products | Ship stub templates (documented pattern) for both `StatusDetectable`-only and `Setupable` providers; enforce via code review; Area 7 replaces stubs incrementally with a documented minimum bar established first |
| Breaking change to `gcx setup status` output shape (instrumentation-only → all-providers aggregated) | CI/automation parsing the old fixed PRODUCT/ENABLED/HEALTH/DETAILS table will break silently | Release notes documenting the change; agent-mode JSON contract is stable and preferred for automation; `gcx setup instrumentation status` remains for the narrower query |
| Interactive prompt library choice adds dependency weight or conflicts with gcx style system | Slower binary, larger surface, or clashes with internal/style theming | Implement minimal widgets in-repo (text/bool/choice/multi_choice/secret) using bufio + termios-style input masking; keep the door open to adopt a small, vetted library later without changing the orchestration contract |
| Partial-completion state in `gcx setup run` confuses users (some providers succeed, some fail, some skipped, some cancelled) | Users re-run and are unsure of resulting state; perceived unreliability | Render a clear post-run summary: per-provider outcome (completed / failed / skipped / cancelled) + explicit "re-run `gcx setup run` to retry un-configured providers" message; post-run status table confirms ground truth |
| Stub `Status()` using only `ConfigKeys()` presence can misreport a provider as `configured` when the upstream API is unreachable | Users see a green-ish row while the product is actually broken | Document the stub heuristic explicitly in the status `Details` column (e.g., "config keys present; no API probe performed"); Area 7 replaces stubs with real probes and the `Details` copy flips automatically |
| Per-provider setup commands that must exist but have no real implementation (stubs) are discoverable in `--help` and may mislead users | Users run `gcx synth setup` and get `ErrSetupNotSupported` | Stub command help text explicitly marks the flow as "not yet implemented"; non-zero exit code + stderr message on invocation; tracking issue per provider for Area 7 replacement |

## Open Questions

- [RESOLVED]: What is the default per-provider timeout for `Status()` aggregation? — 5 seconds (locked in plan.md Design Decisions; matches typical gcx HTTP call budget). Flag-based override is deferred to a later iteration.
- [DEFERRED]: Field-level validation error targeting (structured error protocol between `ValidateSetup` and the orchestrator to re-prompt only the bad field). v1 re-prompts all fields with previous values as defaults. Revisit if providers demand it (ADR §Rejected Alternatives).
- [DEFERRED]: `ConcurrencySafe() bool` method or `--parallel` flag on `gcx setup run`. v1 is strictly sequential `Setup()` execution. Revisit if two provably independent long-running setup flows emerge (ADR §Rejected Alternatives).
- [DEFERRED]: Rich choice metadata (`[]Choice{Value, Description}` from `ResolveChoices`). v1 uses `[]string`. Evolve when a provider needs descriptions alongside values (ADR §Rejected Alternatives).
- [DEFERRED]: Dependency-aware provider ordering (e.g., instrumentation before metrics/logs). v1 uses alphabetical ordering. Revisit by adding `Dependencies() []string` or a priority field on `InfraCategory` if needs emerge (ADR §Rejected Alternatives).
- [DEFERRED]: `--dry-run` / `--show-commands` preview modes on `gcx setup run` that render equivalent `gcx <provider-area> setup` commands instead of executing. Power-user feature, not v1 (ADR §Rejected Alternatives).
- [DEFERRED]: `gcx setup doctor` deeper health checks (token expiry, API version compatibility). Future follow-up listed in ADR §Follow-up Work.
