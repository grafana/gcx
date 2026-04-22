---
type: feature-spec
title: "Login & Config Consolidation"
status: done
research: docs/adrs/login-consolidation/001-login-config-consolidation.md
created: 2026-04-17
---

# Login & Config Consolidation

## Problem Statement

First-time users of `gcx` currently take 6-8 manual steps before producing any useful output: three separate `gcx config set` calls to populate server URL, API token, and namespace; then `gcx auth login` for OAuth; then `gcx config use-context` to activate the new context; finally `gcx config check` to validate. The conceptual boundary between `gcx auth`, `gcx config`, and `gcx setup` is unclear — authentication is semantically part of configuration but lives as a separate top-level command.

Affected users:
- **New users** attempting first-time onboarding against Grafana Cloud or on-prem Grafana 12+.
- **Operators rotating credentials or switching stacks**, who must re-enter a multi-step flow.
- **CI/agent integrations** that want a single idempotent "persist credentials to a config file" entry point.

The current workaround is a documented sequence of commands in the onboarding guide; the pain is the step count, the ordering requirement, and the undiscoverability of `gcx auth` as a config-adjacent concept. This spec implements ADR 001 (Login & Config Consolidation, status accepted) which consolidates the flow into a single `gcx login` command that collapses the entire onboarding into one guided interaction while preserving kubectl-style named contexts and env-var precedence.

## Scope

### In Scope

- New `internal/login` package exposing `Run(ctx, Options) (Result, error)` orchestrating the full login lifecycle (server URL resolution, context name derivation, target detection, Grafana auth, Cloud API auth, connectivity validation, config persistence).
- New `cmd/gcx/login/` Cobra command providing thin CLI wiring, flag parsing, and interactive prompts (via `charmbracelet/huh`) that feed into `login.Options` and call `login.Run()`.
- Cloud vs on-prem target detection heuristic based on known Grafana Cloud domains, loopback, RFC 1918 private IPv4, and IPv6 ULA ranges; unauthenticated `/api/frontend/settings` probe for ambiguous URLs before falling back to an interactive prompt.
- Two-step authentication flow: Step 1 Grafana auth (OAuth PKCE or Grafana API token / service-account token); Step 2 Cloud API auth (Cloud Access Policy token via GCOM) — skippable for Cloud targets.
- Connectivity validation against `/api/health`, K8s discovery `/apis`, `/api/frontend/settings` (Grafana version must be >= 12), and GCOM `GET /api/instances/{slug}` when a Cloud target has a CAP token.
- One additive config field: `GrafanaConfig.AuthMethod` (`json:"auth-method,omitempty"`) with values `"oauth"`, `"token"`, `"basic"`; empty means legacy / inferred.
- Hard removal of `cmd/gcx/auth/` — the project is pre-GA so there is no migration obligation; `gcx login` is the direct replacement.
- `gcx config check` updated to display the `auth-method` field from the active context, with inference fallback when the field is empty.
- `--yes` / agent-mode non-interactive mode that errors with a structured missing-fields message rather than prompting or hanging.
- Env-var precedence unchanged (`GRAFANA_SERVER`, `GRAFANA_TOKEN`, `GRAFANA_CLOUD_TOKEN`) — resolved by the existing config loader at read time.
- `NeedInput` / `NeedClarification` sentinel errors emitted by `internal/login` to keep the package UI-free; CLI layer catches, prompts, and retries `Run()`.
- Unit tests for `internal/login` using table-driven patterns with an injected `NewAuthFlow` factory and mocked connectivity checks.

### Out of Scope

- OAuth coverage of Cloud APIs (issues #310/#311). Step 2 will still require a separate CAP token; when OAuth eventually covers Cloud APIs, Step 2 can be removed.
- Changes to GCOM client, `internal/auth.Flow`, `internal/grafana.Client`, or the K8s discovery registry beyond new call sites.
- Changes to `CloudConfig.APIUrl` behavior or `ResolveGCOMURL()` semantics.
- Namespace selection UX beyond passing through an existing `--namespace` flag to `login.Options` (if present) — this spec focuses on server/auth/context.
- Non-Grafana authentication providers (SAML, OIDC outside the built-in Grafana OAuth flow).

## Key Decisions

| Decision | Chosen | Rationale | Source |
|---|---|---|---|
| Where does login logic live? | New `internal/login` package with `Run(ctx, Options) (Result, error)` | CONSTITUTION: `cmd/` is CLI wiring only; unit-testable via injected deps | ADR §Decision, Rejected Alternative A |
| How are prompts handled without leaking UI into `internal/`? | `NeedInput` / `NeedClarification` sentinel errors; CLI catches, prompts via `huh`, retries `Run()` | Keeps `internal/login` pure; enables deterministic unit tests | ADR §3 |
| How is Cloud vs on-prem detected? | `stackSlugFromServerURL()` for known Cloud domains; loopback + RFC 1918 + IPv6 ULA for on-prem; else Unknown | Reuses existing helpers; avoids over-matching `.local`/`.internal`/`.corp`/`.lan` | ADR §4 |
| How is the ambiguous-URL case resolved? | Interactive prompt; `--cloud` / `--cloud-token` forces Cloud; `--yes` / agent mode defaults to on-prem unless `--cloud` supplied | Safe default for automation; cheap prompt interactively | ADR §4 |
| Is `gcx auth` removed? | Hard removed — `cmd/gcx/auth/` package deleted; no shim | Project is pre-GA; no migration obligation; clean removal is simpler than a shim that would be dead code | Project pre-GA status |
| How is auth method recorded in config? | New `AuthMethod string` in `GrafanaConfig` with `json:"auth-method,omitempty"` | Additive, backward compatible; empty = legacy/inferred | ADR §7 |
| How do CI/agents authenticate without the new command? | Env vars (`GRAFANA_SERVER`, `GRAFANA_TOKEN`, `GRAFANA_CLOUD_TOKEN`) resolved by config loader; `gcx login --yes --server X --token Y --cloud-token Z` for durable config | Env path unchanged; `--yes` gives explicit persist path | ADR §8 |
| What validates "connected"? | `/api/health` + K8s `/apis` discovery + `grafana.GetVersion()` >= 12; GCOM `GetStack(slug)` when Cloud+CAP | Proves both tiers reachable and server is v12+ | ADR §9 |
| What happens when validation fails? | Context NOT saved; clear error identifying the failed check | Avoids half-configured contexts | ADR §9 |
| Which interactive library? | `charmbracelet/huh` (existing direct dep) | Matches pattern in `cmd/gcx/dev/scaffold.go:askMissingOpts`; no new dep | ADR §10 |
| Agent mode / non-TTY behavior? | `agent.IsAgentMode()` or non-TTY stdin => structured error listing missing fields; `--yes` safe in scripts | TTY requirement of `huh`; avoids hangs | ADR §10 |

## Functional Requirements

### Package and command structure

- **FR-001**: The implementation MUST introduce a new `internal/login` package containing at least `login.go` (exposing `Run`, `Options`, `Result`), `detect.go` (target detection), `validate.go` (connectivity validation), and `login_test.go` (table-driven tests).
- **FR-002**: `internal/login` MUST NOT import `cmd/`, MUST NOT call any interactive prompt library, and MUST NOT read from `os.Stdin` or write user-facing prompts to `os.Stderr` outside the `Options.Writer` field.
- **FR-003**: `cmd/gcx/login/` MUST contain only Cobra wiring: flag registration, `Options` population from flags/env, interactive prompting (`huh` forms), invocation of `login.Run()`, and translation of `Result` into exit status and user-facing output.
- **FR-004**: `login.Options` MUST include at minimum: `Server string`, `ContextName string`, `Target Target`, `GrafanaToken string`, `CloudToken string`, `CloudAPIURL string`, `UseOAuth bool`, `Yes bool`, `ConfigSource config.Source`, `NewAuthFlow func(server string, opts auth.Options) AuthFlow`, and `Writer io.Writer`.
- **FR-005**: `login.Result` MUST include at minimum: `ContextName string`, `AuthMethod string` (one of `"oauth"`, `"token"`, `"basic"`), `IsCloud bool`, `HasCloudToken bool`, `GrafanaVersion string`, `Capabilities []string`.

### Target detection

- **FR-006**: When `Options.Target == TargetUnknown`, `login.Run()` MUST attempt detection in the following order: (a) known Grafana Cloud domains (`*.grafana.net`, `*.grafana-dev.net`, `*.grafana-ops.net`) via `stackSlugFromServerURL()` classify as `TargetCloud`; (b) hostnames `localhost`, `127.0.0.1`, `::1`, `*.localhost`, RFC 1918 private IPv4 ranges, and IPv6 ULA (`fd00::/8`) classify as `TargetOnPrem`; (c) attempt an unauthenticated `GET {server}/api/frontend/settings` probe (short timeout, via `httputils`) — if the response contains Cloud-specific markers in `buildInfo` or plugin metadata, classify as `TargetCloud`; if the server is definitively non-Cloud based on the response, classify as `TargetOnPrem`; (d) if the probe is inconclusive or fails, return a `NeedClarification` sentinel error.
- **FR-007**: Detection MUST NOT classify `.local`, `.internal`, `.corp`, or `.lan` suffixes as on-prem.
- **FR-008**: When target is Unknown AND (`Options.Yes` is true OR `agent.IsAgentMode()` returns true), `login.Run()` MUST default to `TargetOnPrem` UNLESS `--cloud` or `--cloud-token` was supplied (in which case `TargetCloud`).

### Authentication flow

- **FR-009**: Step 1 (Grafana auth) MUST select a method in this priority order: (a) `Options.GrafanaToken` non-empty => token auth, persisted to `GrafanaConfig.APIToken`, `AuthMethod="token"`; (b) `Options.UseOAuth` true OR interactive Cloud target without `GrafanaToken` => invoke `Options.NewAuthFlow(server, authOpts).Run(ctx)`, persist `OAuthToken`/`OAuthRefreshToken`/`ProxyEndpoint` to `GrafanaConfig`, `AuthMethod="oauth"`; (c) no input available and not `Yes` => return `NeedInput` sentinel identifying the missing Grafana credential.
- **FR-010**: Step 2 (Cloud API auth) MUST run only when effective `Target == TargetCloud`. If `Options.CloudToken` is non-empty, it MUST be persisted to `CloudConfig.Token`; if empty and interactive, CLI MUST prompt with Enter-to-skip semantics; if empty and `Yes`/agent, Step 2 MUST be skipped (context saved without a CAP token).
- **FR-011**: When Step 2 is skipped, subsequent commands that require Cloud APIs MUST error with a message pointing the user to `gcx login --context <name>` to add a CAP token. (Spec note: this FR covers the error-message expectation; implementation is shared with existing Cloud-requiring commands.)
- **FR-012**: The Grafana auth method value written to `GrafanaConfig.AuthMethod` MUST be exactly one of `"oauth"`, `"token"`, `"basic"`. Empty string MUST remain valid and MUST be treated by readers as "infer from populated fields".

### Connectivity validation

- **FR-013**: Before persisting the context, `login.Run()` MUST perform, in order: (a) `GET /api/health` reachability check; (b) K8s API availability via `discovery.NewDefaultRegistry(ctx, cfg)` probing `/apis`; (c) Grafana version check via `grafana.GetVersion(ctx)` — MUST fail if the returned version is below 12.0.0; (d) when `Target == TargetCloud` AND a CAP token was provided, `cloud.GCOMClient.GetStack(ctx, slug)` MUST succeed.
- **FR-014**: If any required validation step fails, `login.Run()` MUST return an error identifying the failed check and MUST NOT write, create, or mutate any context in the config file.

### Config persistence

- **FR-015**: `login.Run()` MUST resolve the context name via `Options.ContextName` when set, else via `config.ContextNameFromServerURL(server)`. An existing context matching the derived name without explicit tokens in `Options` MUST trigger re-auth mode (tokens refreshed; other fields left intact).
- **FR-016**: After successful validation, `login.Run()` MUST load the existing config tolerantly, create or update the named context with server, namespace defaults, tokens, `AuthMethod`, and (for Cloud) CAP token; set the context as the current context; and call `config.Write(ctx, Options.ConfigSource, cfg)`.
- **FR-017**: The new `AuthMethod` field MUST serialize with `json:"auth-method,omitempty"` and `yaml:"auth-method,omitempty"` so existing config files without the field deserialize unchanged.

### Removal and backward compatibility

- **FR-018**: `cmd/gcx/auth/` MUST be deleted. The `gcx auth` top-level command MUST NOT be registered in the root command. Invoking `gcx auth` MUST result in Cobra's default "unknown command" error and a non-zero exit code.
- **FR-019**: All subcommands under `cmd/gcx/auth/` (including `auth login`) MUST be deleted. No compatibility shim or redirect MUST be left in place.
- **FR-020**: Env-var precedence for `GRAFANA_SERVER`, `GRAFANA_TOKEN`, and `GRAFANA_CLOUD_TOKEN` MUST remain unchanged and MUST continue to be resolved by the config loader at read time (not by the login command).

### Interactive and non-interactive UX

- **FR-021**: Interactive mode MUST use `charmbracelet/huh`, composing Form 1 (pre-detect, collects server URL if missing) and Form 2 (post-detect, collects target clarification when Unknown, auth method, and CAP token). Each field MUST be appended only when the corresponding `Options` field is empty; if all values come from flags/env, no form MUST be shown.
- **FR-022**: When `Options.Yes` is true OR `agent.IsAgentMode()` returns true OR stdin is not a TTY, the command MUST NOT invoke any `huh` form and MUST return a structured error (`fail.DetailedError` or `config.ValidationError`) enumerating the missing fields if any required input is unavailable.
- **FR-023**: All HTTP clients introduced by this feature (connectivity checks beyond K8s discovery, GCOM calls, and the ambiguous-URL probe) MUST route through `httputils` (directly or via existing wrappers such as `internal/cloud`).

### Config check

- **FR-024**: `gcx config check` MUST display an `auth-method` row for the active context. When `GrafanaConfig.AuthMethod` is non-empty, its value MUST be shown verbatim. When it is empty, the displayed value MUST be the inferred method: OAuth tokens present → `"oauth"`; `APIToken` only → `"token"`; basic credentials → `"basic"`; no auth fields → `"unknown"`.

### Ambiguous URL probe

- **FR-025**: The unauthenticated `/api/frontend/settings` probe in FR-006(c) MUST complete within a fixed short timeout (≤ 3 seconds). A timeout or connection error MUST be treated as inconclusive (fall through to step (d)). The probe MUST NOT block the rest of the login flow on a slow server.

## Acceptance Criteria

- **AC-001** (First-run Cloud with CAP, interactive):
  - GIVEN an interactive TTY, a clean config, and a user who runs `gcx login`
  - WHEN the user enters `https://example.grafana.net`, completes the OAuth browser flow, and supplies a valid CAP token when prompted
  - THEN the command persists a new context named per `ContextNameFromServerURL`, with `AuthMethod="oauth"`, OAuth tokens populated, `CloudConfig.Token` set, sets it current, and exits 0 with a success message reporting the Grafana version and stack name.

- **AC-002** (First-run Cloud without CAP, skip Step 2):
  - GIVEN an interactive TTY and a clean config
  - WHEN the user runs `gcx login`, supplies `https://example.grafana.net`, completes OAuth, and presses Enter at the CAP token prompt to skip
  - THEN the context is saved with OAuth tokens and `AuthMethod="oauth"`, `CloudConfig.Token` remains empty, and the command exits 0 with a note that Cloud API commands will require re-running `gcx login --context <name>`.

- **AC-003** (First-run on-prem with token):
  - GIVEN a localhost Grafana 12 instance and a clean config
  - WHEN the user runs `gcx login --server http://localhost:3000 --token <sa-token>` in an interactive or non-interactive shell
  - THEN target is detected as on-prem without prompting, no Step 2 runs, `GrafanaConfig.APIToken` is set, `AuthMethod="token"`, and the command exits 0.

- **AC-004** (Ambiguous URL, interactive):
  - GIVEN a custom domain `https://grafana.example.com` and an interactive TTY
  - WHEN the user runs `gcx login --server https://grafana.example.com`
  - THEN the command prompts "Is this Grafana Cloud?" and, based on the answer, proceeds with either one-step (on-prem) or two-step (Cloud) auth.

- **AC-005** (Ambiguous URL, `--yes` without `--cloud`):
  - GIVEN a custom domain and a non-interactive shell
  - WHEN the user runs `gcx login --yes --server https://grafana.example.com --token <token>`
  - THEN the command defaults to on-prem (no Step 2), persists the context, and exits 0.

- **AC-006** (Ambiguous URL, `--yes --cloud`):
  - GIVEN a custom domain and a non-interactive shell
  - WHEN the user runs `gcx login --yes --server https://grafana.example.com --token <token> --cloud-token <cap>`
  - THEN the command classifies the target as Cloud, persists both tokens, and exits 0.

- **AC-007** (CI via env vars only, no login):
  - GIVEN `GRAFANA_SERVER`, `GRAFANA_TOKEN`, and `GRAFANA_CLOUD_TOKEN` exported in the environment and no config file changes
  - WHEN a CI job runs any non-login gcx command (e.g. `gcx resources get`)
  - THEN the config loader resolves these env vars at read time and the command succeeds without any prior `gcx login` invocation.

- **AC-008** (Agent mode: no prompt, structured error):
  - GIVEN `GCX_AGENT_MODE=true` (or any of the detected agent env vars) and a shell with no flags/env supplying the server URL
  - WHEN the user runs `gcx login`
  - THEN no `huh` form is shown, and the command exits non-zero with a structured error listing the missing field(s) (e.g. `server`) and suggesting the corresponding flag or env var.

- **AC-009** (Re-auth existing context):
  - GIVEN an existing context whose OAuth token has expired and no explicit token flags supplied
  - WHEN the user runs `gcx login --context <existing-name>` interactively
  - THEN the command re-runs the appropriate auth step, updates only the token fields and `AuthMethod` on the existing context (leaving other fields intact), and exits 0.

- **AC-010** (`gcx auth` removed):
  - GIVEN a user invoking `gcx auth login` or any other `gcx auth` subcommand
  - WHEN the CLI parses the command
  - THEN the CLI exits non-zero with Cobra's default "unknown command" error; no context is written; no authentication is attempted.

- **AC-011** (`auth-method` field written and read):
  - GIVEN a successful `gcx login` with OAuth
  - WHEN the resulting config file is read back
  - THEN the active context's `grafana.auth-method` field equals `"oauth"`, and a subsequent `gcx` command that reads this config treats the auth method as `"oauth"` without inference.

- **AC-012** (Legacy config without `auth-method`):
  - GIVEN a pre-existing config file that does not contain any `auth-method` field
  - WHEN gcx loads the config
  - THEN deserialization succeeds, the missing field is treated as empty, and auth-method is inferred from populated fields (OAuth tokens => oauth; APIToken only => token; basic creds => basic).

- **AC-013** (Validation failure — no half-configured context):
  - GIVEN a server that responds to `/api/health` but reports Grafana version 11.x via `/api/frontend/settings` (or fails K8s discovery, or returns GCOM 401 for the stack)
  - WHEN `gcx login` runs
  - THEN the command exits non-zero with an error naming the failed validation step, and the config file is NOT modified (no new context, no updated `CurrentContext`).

- **AC-014** (Cloud detection without prompt for known domains):
  - GIVEN a server URL matching `*.grafana.net`, `*.grafana-dev.net`, or `*.grafana-ops.net`
  - WHEN `gcx login --server <url>` is invoked in any mode (interactive, `--yes`, agent)
  - THEN target is classified as Cloud without any target-clarification prompt.

- **AC-015** (`internal/login` is UI-free):
  - GIVEN a unit test importing `internal/login`
  - WHEN it invokes `login.Run(ctx, Options{Writer: &buf})` with all required fields populated and an injected `NewAuthFlow` returning a stub `AuthFlow`
  - THEN the test completes without reading `os.Stdin`, without touching `huh`, and any user-facing output is captured only via `Options.Writer`.

- **AC-016** (`gcx config check` displays `auth-method`):
  - GIVEN an active context where `GrafanaConfig.AuthMethod` is `"oauth"`
  - WHEN the user runs `gcx config check`
  - THEN the output includes an `auth-method: oauth` row (or equivalent key-value line) for the active context.

- **AC-017** (`gcx config check` infers `auth-method` for legacy configs):
  - GIVEN an active context where `GrafanaConfig.AuthMethod` is empty but `GrafanaConfig.APIToken` is set
  - WHEN the user runs `gcx config check`
  - THEN the output displays `auth-method: token` (inferred) rather than an empty or missing value.

- **AC-018** (Ambiguous URL auto-detected via `/api/frontend/settings` probe):
  - GIVEN a custom domain `https://grafana.example.com` that serves identifiable Cloud-specific indicators in `/api/frontend/settings`
  - WHEN `gcx login --server https://grafana.example.com` is invoked
  - THEN the target is classified as Cloud automatically without any target-clarification prompt, and the two-step auth flow proceeds.

- **AC-019** (Probe timeout falls through gracefully):
  - GIVEN a custom domain where the `/api/frontend/settings` probe times out or returns a network error
  - WHEN `gcx login --server https://grafana.example.com` is invoked in interactive mode
  - THEN the command falls through to the "Is this Grafana Cloud?" prompt as if no probe had been attempted.

## Negative Constraints

- **NC-001**: `internal/login` MUST NEVER import `cmd/`, `charmbracelet/huh`, or read from `os.Stdin`. Any need for user input MUST be surfaced as a `NeedInput` / `NeedClarification` sentinel error for the CLI layer to handle.
- **NC-002**: The implementation MUST NEVER write, create, or update a context in the config file when any required connectivity validation fails. Partial / half-configured contexts on failure are forbidden.
- **NC-003**: The implementation MUST NEVER introduce a breaking change to the config schema. The new `AuthMethod` field MUST be additive with `omitempty`; renaming, removing, or altering the serialization of existing fields (`APIToken`, `OAuthToken`, `OAuthRefreshToken`, `ProxyEndpoint`, `CloudConfig.Token`, `CloudConfig.APIUrl`) is forbidden.
- **NC-004**: The implementation MUST NEVER resolve env vars for `GRAFANA_SERVER`, `GRAFANA_TOKEN`, or `GRAFANA_CLOUD_TOKEN` inside `cmd/gcx/login/` or `internal/login/`; resolution MUST remain in the existing config loader at read time.
- **NC-005**: The implementation MUST NEVER bypass `httputils` for HTTP clients introduced by this feature (the one exemption remains the K8s tier, which continues to use `k8s.io/client-go` through the existing discovery registry).
- **NC-006**: The implementation MUST NEVER relax or broaden `auth.ValidateEndpointURL()`'s allowed OAuth callback hosts (`*.grafana.net`, `*.grafana-dev.net`, `*.grafana-ops.net`, `localhost`). Any need to trust additional endpoints is out of scope.
- **NC-007**: The CLI MUST NEVER hang waiting for interactive input when `Options.Yes` is true, `agent.IsAgentMode()` is true, or stdin is not a TTY. Missing required inputs in these modes MUST cause a structured error and a non-zero exit.
- **NC-008**: Since the project is pre-GA, `gcx auth` MUST be hard-removed with no compatibility shim. Post-GA releases will follow a deprecation-then-removal policy; this is the last opportunity to remove `gcx auth` without a migration window.
- **NC-009**: Target detection MUST NOT classify `.local`, `.internal`, `.corp`, or `.lan` hostnames as on-prem (these are ambiguous enterprise-intranet suffixes and must fall through to the Unknown / clarification path).
- **NC-010**: The `CurrentContext` of the config MUST NOT be changed if context save fails at any point; the pre-login current context MUST remain intact on failure.

## Risks

| Risk | Impact | Mitigation |
|---|---|---|
| OAuth endpoint trust list (`auth.ValidateEndpointURL`) narrows usable hosts to Grafana Cloud + localhost — custom/vanity Cloud-backed domains cannot use OAuth today | Users on custom domains must fall back to token auth even when OAuth would be preferable | Document this explicitly in `gcx login` help; surface as a dedicated error when OAuth is requested against a non-allowlisted host; revisit the allowlist in a separate ADR if demand grows |
| Ambiguous-URL clarification prompt becomes noisy for users with many custom domains | UX friction; repeated prompts for the same domain across re-auths | Interactive only when Unknown; `--yes`/agent mode defaults on-prem (safe); context name is cached, so re-auth of an existing context inherits the prior target classification |
| `AuthMethod` field duplicates what can be inferred from populated auth fields | Minor data duplication; risk of drift between `AuthMethod` and populated fields | Readers treat empty `AuthMethod` as "infer"; tests assert the two views are consistent after every write; `config check` (follow-up) surfaces the stored value for user verification |
| Agent-mode error ergonomics: agents / CI may find structured missing-field errors harder to act on than a prompt | Initial failed runs for new agent integrations | Error message enumerates every missing field with the exact flag and env var that would satisfy it; examples in the command's long help include a full non-interactive invocation |
| `gcx auth` hard removal breaks existing scripts | Pre-GA users with `gcx auth` in scripts get broken installs on upgrade | Pre-GA status means no backward-compat guarantee; communicate removal clearly in the changelog; `gcx login` is a direct drop-in replacement; document migration path |
| `/api/frontend/settings` probe heuristic is fragile if Cloud response format changes | Silent mis-classification of ambiguous URLs | Probe is opportunistic — inconclusive result falls through to the prompt; false negatives are safe (prompt shown); false positives could trigger Cloud auth unexpectedly; document the probe heuristic and make it easy to override with `--cloud`/`--on-prem` flags |
| Connectivity validation slows the login path | Perceived sluggishness vs the old flow | Checks are inherently needed to guarantee a working context; tune for parallelism where safe (e.g. GCOM probe can run concurrently with version check); surface a spinner via `huh` so the wait is legible |
| Re-auth mode's "tokens refreshed, other fields intact" semantics might silently overwrite manually edited context fields | Users with custom-edited contexts may be surprised | Re-auth touches only token fields and `AuthMethod`; document the exact set of fields written in the command help and ADR |

## Open Questions

- **[RESOLVED]** Should login orchestration live in `internal/config` or a new package? — New `internal/login` package (ADR §Decision; Rejected Alternative C).
- **[RESOLVED]** Should `gcx auth` be hard-removed? — Yes; project is pre-GA, no migration obligation; `gcx auth` is removed in this change (2026-04-17 decision).
- **[RESOLVED]** How are prompts kept out of `internal/login`? — `NeedInput` / `NeedClarification` sentinel errors handled by the CLI layer (ADR §3).
- **[RESOLVED]** Is the new `AuthMethod` field backward compatible? — Yes: additive, `omitempty`, legacy configs infer from populated fields (ADR §7).
- **[RESOLVED]** How should CI/agents authenticate without running `gcx login`? — Via existing env vars (`GRAFANA_SERVER`, `GRAFANA_TOKEN`, `GRAFANA_CLOUD_TOKEN`) resolved by the config loader at read time (ADR §8).
- **[RESOLVED]** What hostnames classify as on-prem without prompting? — Loopback, `*.localhost`, RFC 1918 IPv4, IPv6 ULA (`fd00::/8`); explicitly excludes `.local`/`.internal`/`.corp`/`.lan` (ADR §4).
- **[RESOLVED]** Which interactive library is used? — `charmbracelet/huh`, same pattern as `cmd/gcx/dev/scaffold.go:askMissingOpts` (ADR §10).
- **[RESOLVED]** Should `/api/frontend/settings` probing be included in this spec? — Yes; moved from post-MVP to in-scope (2026-04-17 decision).
- **[RESOLVED]** Should `gcx config check` display `auth-method`? — Yes; moved from post-MVP to in-scope (2026-04-17 decision).
- **[DEFERRED]** When OAuth covers Cloud APIs (issues #310/#311), remove Step 2 (CAP token) entirely — tracked upstream; revisit when those issues land (ADR §Follow-up Work).
