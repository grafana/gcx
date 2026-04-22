---
type: feature-plan
title: "Login & Config Consolidation"
status: draft
spec: docs/specs/feature-login-config-consolidation/spec.md
created: 2026-04-17
---

# Architecture and Design Decisions

## Pipeline Architecture

### `login.Run()` end-to-end flow

```
                        cmd/gcx/login/command.go
                                |
                                v
  +-------------------------------------------------------------+
  |                       internal/login.Run(ctx, opts)          |
  +-------------------------------------------------------------+
            |
            v
  [1] Resolve server URL
        opts.Server (flag)
          |  env: GRAFANA_SERVER (resolved by config loader, already in opts)
          |  empty -> return ErrNeedInput{Fields: ["server"]}
          v
  [2] Derive context name
        opts.ContextName if set
          else config.ContextNameFromServerURL(server)   // internal/config/context.go:168
          detect re-auth: context already exists in config file
          v
  [3] Detect target                                       (internal/login/detect.go)
        opts.Target != TargetUnknown  -> use as-is
        stackSlugFromServerURL(url) ok -> TargetCloud
        isLocalHostname(host)          -> TargetOnPrem
        else: probe GET /api/frontend/settings (unauthenticated, ≤3s, via httputils)
          Cloud indicators in response   -> TargetCloud (no prompt)
          definitively not-Cloud         -> TargetOnPrem (no prompt)
          timeout/error/inconclusive     -> TargetUnknown
          if TargetUnknown:
            opts.Yes + !opts.Cloud + !opts.CloudToken -> default to TargetOnPrem
            opts.Cloud || opts.CloudToken            -> TargetCloud
            else                                     -> ErrNeedClarification{Q:"cloud?"}
          v
  [4] Step 1 - Grafana auth
        opts.GrafanaToken        -> AuthMethod="token", store APIToken
        opts.UseOAuth || Cloud+i -> NewAuthFlow(server).Run(ctx) -> AuthMethod="oauth"
                                    internal/auth.Flow (OAuth PKCE, Cloud-only)
        else                     -> ErrNeedInput{Fields: ["grafana-auth"]}
          v
  [5] Step 2 - Cloud API auth   (only if Target == TargetCloud)
        opts.CloudToken set      -> stage CloudConfig.Token
        not set + interactive    -> ErrNeedInput{Fields: ["cloud-token"], Optional: true}
        empty response / --yes   -> skip (context created without CAP)
          v
  [6] Validate connectivity                              (internal/login/validate.go)
        grafana.Client.Health()                 (/api/health)
        discovery.NewDefaultRegistry(rest.Config)  (K8s /apis)
        grafana.GetVersion() >= 12              (/api/frontend/settings)
        if Cloud && CloudToken:
            cloud.GCOMClient.GetStack(slug)     (validates CAP, fetches stack metadata)
        any failure -> return error (no partial save)
          v
  [7] Persist config
        load existing (tolerant)
        upsert Context{ Grafana, Cloud }
        set GrafanaConfig.AuthMethod = Result.AuthMethod
        CurrentContext = ContextName
        config.Write(source)
          v
  [8] Return Result{ ContextName, AuthMethod, IsCloud, HasCloudToken,
                     GrafanaVersion, Capabilities }
```

### Command / package topology

```
                    +---------------------------------+
                    |         cmd/gcx/login/          |
                    |  command.go  (Cobra wiring)     |
                    |  - flag parsing                 |
                    |  - huh interactive forms        |
                    |  - Run()-retry loop on sentinels|
                    +----------------+----------------+
                                     |
                                     v
  +-----------------------+    +-----+-----+    +---------------------------+
  | cmd/gcx/auth/         |--->|           |    |  internal/agent           |
  | (Deprecated=true,     |    | internal/ |--->|  IsAgentMode() / TTY check|
  |  delegates to login)  |    | login     |    +---------------------------+
  +-----------------------+    | Run()     |
                               | Options   |    +---------------------------+
                               | Result    |--->|  internal/auth            |
                               | detect.go |    |  Flow (OAuth PKCE)        |
                               | validate  |    |  ValidateEndpointURL      |
                               +-----+-----+    +---------------------------+
                                     |
                +--------------------+--------------------+
                |                    |                    |
                v                    v                    v
  +-----------------------+  +-----------------+  +----------------------+
  | internal/config       |  | internal/cloud  |  | internal/grafana     |
  | - Load/Write          |  | GCOMClient.     |  | Client.Health()      |
  | - Context types       |  |   GetStack()    |  | GetVersion()         |
  | - ContextName...URL() |  +-----------------+  +----------------------+
  | - stackSlug...URL()   |
  | - AuthMethod field    |  +----------------------------------------+
  +-----------------------+  | internal/resources/discovery           |
                             | NewDefaultRegistry (K8s /apis probe)   |
                             +----------------------------------------+

  HTTP tiers used by validation all go through internal/httputils
  (except the K8s discovery registry, which uses client-go).
  Interactive prompts use github.com/charmbracelet/huh in cmd/ only.
```

## Design Decisions

| # | Decision | Rationale | Traces to |
|---|----------|-----------|-----------|
| D1 | New `internal/login` package owns orchestration; `cmd/gcx/login` is thin Cobra + huh wiring. | Keeps domain logic unit-testable and enforces CONSTITUTION layer separation (cmd/ is CLI wiring only). Rejected "thin orchestrator" and "config-centric" alternatives from ADR. | FR-001, NC-001, AC-015 |
| D2 | `login.Run(ctx, Options) (Result, error)` is the single public entry point. | One function, one flow; easy to call from both `gcx login` and the legacy `gcx auth login` shim. | FR-001, FR-002 |
| D3 | Options pre-declared in spec: `Server`, `ContextName`, `Target`, `GrafanaToken`, `CloudToken`, `CloudAPIURL`, `UseOAuth`, `Yes`, `ConfigSource`, `NewAuthFlow`, `Writer`. | Every CLI flag/env input has a field; injection points (`ConfigSource`, `NewAuthFlow`, `Writer`) enable unit tests without real filesystem, browser, or network. | FR-004, FR-003 |
| D4 | Result carries `ContextName`, `AuthMethod`, `IsCloud`, `HasCloudToken`, `GrafanaVersion`, `Capabilities`. | Callers (and future `gcx config check`) can render consistent post-login summary; `AuthMethod` round-trips to config. | FR-005 |
| D5 | Target detection order: explicit flag -> `stackSlugFromServerURL` -> `isLocalHostname` -> unauthenticated `/api/frontend/settings` probe -> `TargetUnknown`. | Probe reduces prompts for custom Cloud domains; falls through safely on error/timeout; excludes `.local/.internal/.corp/.lan` on purpose. | FR-006, FR-007, FR-025, NC-009 |
| D6 | `NeedInput` / `NeedClarification` sentinel errors returned from `Run()`; CLI layer prompts and retries. | `internal/login` stays UI-free (no huh import, no stdin read), satisfying NC-001 and enabling table-driven tests. CLI catches sentinels, runs a `huh` form, and re-invokes `Run` with filled-in options. | FR-002, FR-021, NC-001, AC-015 |
| D7 | Two-step auth orchestrated sequentially: Grafana auth first (step 1), then Cloud API (step 2) only on `TargetCloud`. | Step 1 is required for any command; step 2 is gated on target and is skippable via empty response. Matches existing config (`GrafanaConfig.OAuth*` or `APIToken` vs `CloudConfig.Token`). | FR-009, FR-010, AC-001, AC-002 |
| D8 | Step 1 priority: `GrafanaToken` (SA) > `UseOAuth` flag > interactive OAuth for Cloud targets > `NeedInput` otherwise. | Respects explicit user choice; OAuth stays Cloud-only because `internal/auth.ValidateEndpointURL` restricts callback hosts. Avoids an unreachable OAuth path for on-prem. | FR-009, AC-003, NC-006 |
| D9 | Step 2: `CloudToken` stored in existing `CloudConfig.Token`; empty input skips step 2 without erroring. | Reuses existing schema; skipping keeps the context usable for non-Cloud features while allowing late-binding via `gcx login --context X` later. | FR-010, FR-011, AC-002 |
| D10 | Ambiguous URL resolution: interactive prompt, `--cloud`/`--cloud-token` forces Cloud, `--yes` alone defaults to on-prem. | CI never hangs; agents get deterministic behavior; interactive users get the one-question UX without guessing wrong. | FR-008, AC-004, AC-005, AC-006, NC-007 |
| D11 | Connectivity validation order: `/api/health` -> K8s discovery registry -> `GetVersion() >= 12` -> (if Cloud+CAP) `GCOMClient.GetStack`. | Sequential for clearer error messages and to fail fast on the cheapest check. All checks reuse existing clients (`internal/grafana`, `internal/resources/discovery`, `internal/cloud`) — no new HTTP code. | FR-013, FR-023 |
| D12 | Validation failure short-circuits before config write; no `config.Write` call on any error. | Prevents half-configured contexts and leaves `CurrentContext` untouched. Satisfied by placing Write as the last side-effect after all checks pass. | FR-014, AC-013, NC-002, NC-010 |
| D13 | Config schema change: add `GrafanaConfig.AuthMethod string \`json:"auth-method,omitempty"\`` only. | Smallest additive change needed to know re-auth method without guessing from populated fields. `omitempty` means zero wire diff for legacy configs. | FR-012, FR-017, AC-011, AC-012, NC-003 |
| D14 | Existing env-var resolution (`GRAFANA_SERVER`, `GRAFANA_TOKEN`, `GRAFANA_CLOUD_TOKEN`, `GRAFANA_CLOUD_API_URL`) stays in the config loader; `login` consumes already-resolved Options. | Keeps precedence rules identical to every other command; `gcx login` is just another config writer, not a special case. | FR-020, AC-007, NC-004 |
| D15 | `cmd/gcx/auth/` package hard-removed; no compatibility shim. | Project is pre-GA — no migration obligation; clean removal avoids dead shim code; `gcx login` is the direct replacement. | FR-018, FR-019, AC-010, NC-008 |
| D21 | Unauthenticated `/api/frontend/settings` probe inserted between `isLocalHostname` and `ErrNeedClarification` in the detection chain. | Reduces interactive prompts for custom Cloud domains; probe timeout is ≤ 3s and inconclusive results fall through safely to the existing prompt path. | FR-006, FR-025, AC-018, AC-019 |
| D22 | `gcx config check` enhanced to display `auth-method` with inference fallback when `GrafanaConfig.AuthMethod` is empty. | Closes observability gap — users can verify their auth method without reading raw config YAML; inference is deterministic from existing fields. | FR-024, AC-016, AC-017 |
| D16 | Interactive forms use `github.com/charmbracelet/huh`; built dynamically based on which `opts` are empty (mirrors `cmd/gcx/dev/scaffold.go` `askMissingOpts`). | Reuses the house pattern; no forms at all when flags/env supply everything. | FR-021 |
| D17 | Non-TTY / `agent.IsAgentMode()` / `opts.Yes=true` paths never invoke `huh`. On sentinel errors the CLI wraps and returns a structured error listing missing fields. | Safe for scripts and agent consumers; agent clients can machine-read the missing-fields list. | FR-022, AC-005, AC-008, NC-007 |
| D18 | All HTTP clients used during validation route through `internal/httputils` (existing `grafana.Client`, `cloud.GCOMClient`). The K8s discovery path continues to use `client-go` per CONSTITUTION exception. | Upholds CONSTITUTION "all HTTP via httputils except K8s tier". No new raw `http.Client` in `internal/login`. | FR-023, NC-005 |
| D19 | OAuth endpoint trust list (`internal/auth/flow.go` `ValidateEndpointURL`) is untouched. `internal/login` invokes `auth.Flow` as-is. | Honors ADR commitment and keeps the security surface stable. Narrowing or broadening the list is a separate ADR. | NC-006 |
| D20 | Re-auth mode triggered when the context name already exists and no explicit tokens supplied — only token fields and `AuthMethod` are mutated; other context fields left intact. | Covers the credential-rotation flow (AC-009) without surprising users who hand-edited their contexts. | FR-015, AC-009 |

## Compatibility

### Existing behaviour preserved

- **Env-var precedence unchanged.** `GRAFANA_SERVER`, `GRAFANA_TOKEN`, `GRAFANA_CLOUD_TOKEN`, `GRAFANA_CLOUD_API_URL` are still resolved by the config loader before `login.Run` sees Options. Every command that reads config keeps working identically whether `gcx login` was ever run or not (D14, FR-020).
- **Existing config files load unchanged.** `AuthMethod` is `omitempty`; legacy configs without the field continue to work. Code that needs the method falls back to inferring from populated OAuth/token fields when `AuthMethod == ""` (D13, AC-012).
- **`internal/auth.Flow`, `internal/grafana.Client`, `internal/cloud.GCOMClient`, `internal/resources/discovery` are not modified** (spec Out of Scope). `internal/login` composes them; call sites may be added but signatures stay the same.
- **`CurrentContext` is only mutated on success.** Failed validation leaves the previously-active context as-is (NC-010, AC-013).
- **OAuth endpoint trust list unchanged** — no broadening or narrowing (NC-006).

### Removal — `gcx auth`

The project is pre-GA. `cmd/gcx/auth/` is hard-removed in this change with no compatibility shim. Invoking `gcx auth` after this change results in Cobra's default "unknown command" error (AC-010, NC-008).

Migration: replace `gcx auth login` with `gcx login`. Flag and behavior equivalence is guaranteed by the same `login.Run()` code path (FR-018, FR-019). Communicate in the changelog.

### Newly available capabilities

- **Single-command onboarding** — `gcx login` collapses 6-8 config/auth steps into one interactive flow (or one flag-driven non-interactive call).
- **Structured re-auth** — `gcx login --context my-stack` refreshes credentials for an existing context; `AuthMethod` drives whether OAuth or SA-token prompt is used.
- **`--cloud-api-url` flag** — sets `CloudConfig.APIUrl` inline for internal environments (`grafana-dev.com`, `grafana-ops.com`) without a separate `gcx config set` call.
- **`--cloud` / `--cloud-token` disambiguation** — gives users on vanity domains a one-shot override that works in both interactive and `--yes` modes.
- **Auto Cloud-detection for custom domains** — the `/api/frontend/settings` probe resolves Cloud vs on-prem for vanity domains without prompting; `--cloud`/`--on-prem` are escape hatches when the probe misclassifies.
- **`gcx config check` shows `auth-method`** — users can verify how a context is authenticated without reading raw YAML; inferred for legacy contexts.
- **Machine-readable missing-fields errors** in agent mode — CLI returns a structured error listing which Options fields need values, enabling agent orchestrators to fill gaps and retry.

### Surface additions

- New package: `internal/login/` (login.go, detect.go, validate.go, login_test.go).
- New command: `cmd/gcx/login/command.go`.
- New config field: `GrafanaConfig.AuthMethod` (`omitempty`).
- Removed command: `cmd/gcx/auth/` package deleted.
- No removals. No breaking renames. No provider changes.

## Sentinel Error Contract

`internal/login` never reads stdin and never imports `huh`. It signals missing inputs via typed sentinels that the CLI handles:

```go
type ErrNeedInput struct {
    Fields   []string // e.g. ["server"], ["grafana-auth"], ["cloud-token"]
    Optional bool     // true for cloud-token (empty = skip step 2)
    Hint     string   // human-readable context (shown by CLI)
}

type ErrNeedClarification struct {
    Question string   // e.g. "Is this a Grafana Cloud instance?"
    Choices  []string // e.g. ["cloud", "on-prem"]
    Field    string   // Options field to fill: "Target"
}
```

CLI loop (`cmd/gcx/login/command.go`):

```
for {
    result, err := login.Run(ctx, opts)
    switch e := err.(type) {
    case *login.ErrNeedInput:        // interactive: huh form for e.Fields; agent: return structured err
    case *login.ErrNeedClarification: // interactive: huh.NewSelect for e.Choices
    case nil:                        // success: print result, exit 0
    default:                         // terminal error: print, exit non-zero, no config mutation
    }
}
```

Agent mode (`agent.IsAgentMode()` true or `opts.Yes` true on non-TTY): the CLI does not prompt. It wraps the sentinel into a user-visible error listing missing flags/env vars and exits non-zero (FR-022, NC-007).

## Testing Strategy

Unit tests live at `internal/login/login_test.go` (table-driven).

Injection points (all on `Options`):

- **`ConfigSource config.Source`** — returns an in-memory / tmpfile config so tests assert persisted state without touching the user's real config.
- **`NewAuthFlow func(server string, opts auth.Options) AuthFlow`** — returns a fake implementing the one-method `AuthFlow` interface; lets tests simulate OAuth success/failure without a browser.
- **`Writer io.Writer`** — captured for assertions on user-facing messages (mostly a smoke check — the real UX lives in `cmd/`).

Additional seams:

- **Validation clients** (`grafana.Client`, `cloud.GCOMClient`, discovery registry) are constructed inside `validate.go` via small factory functions that tests can override through the package-private `validator` struct.
- **`DetectTarget(url string) Target`** is a pure function — directly table-tested (Cloud / on-prem / ambiguous / malformed URLs).

Representative test cases (one per AC):

- First-run Cloud with CAP token (AC-001)
- First-run Cloud without CAP (step 2 skipped, no GCOM call) (AC-002)
- On-prem with SA token, OAuth not attempted (AC-003)
- Ambiguous URL + `--yes` defaults to on-prem (AC-005)
- Agent mode returns structured missing-fields error (AC-008)
- Validation failure leaves `CurrentContext` untouched (AC-013)
- `AuthMethod` written on save, round-tripped on re-auth (AC-009, AC-011)
- Legacy config (no `AuthMethod`) loads and re-auths (AC-012)

CLI-level interactive flows (huh form sequencing) are covered by a thin integration test on `cmd/gcx/login/`, not the `internal/login` suite.

## Security Considerations

- **OAuth endpoint trust** — `internal/auth.ValidateEndpointURL` continues to restrict OAuth callback hosts to `*.grafana.net`, `*.grafana-dev.net`, `*.grafana-ops.net`, and `localhost`. `internal/login` does not narrow or broaden this list (NC-006, D19). A custom-domain Cloud user choosing the SA-token path at step 1 is the documented workaround; OAuth on custom domains is future work (#310/#311).
- **CAP token handling** — `CloudToken` is accepted from flag (`--cloud-token`), env (`GRAFANA_CLOUD_TOKEN`, resolved by config loader), or interactive masked-input prompt. It is written only to `CloudConfig.Token` and is validated via `GCOMClient.GetStack` before persistence. No echo, no log line.
- **SA token handling** — same rules. `GrafanaToken` lands in `GrafanaConfig.APIToken`, same field existing code uses. Masked in interactive prompts.
- **Never log tokens.** `internal/login` never writes tokens to `Writer`, never includes them in error messages, and never stores them outside the config struct (which is passed to `config.Write` using the existing secrets redaction in `internal/secrets` on read-paths).
- **Half-configured contexts are impossible.** Config write is the last step after all validation passes (D12, NC-002, NC-010). A failed login does not leak credentials into the config file.
- **Env-var precedence unchanged** (NC-004) — no opportunity for `gcx login` to shadow or override env-var-driven credentials differently than the rest of the CLI.

## Follow-up Work

Scoped out of this spec:

- Extend OAuth coverage to Cloud APIs (#310/#311). When done, step 2 of `login.Run` becomes optional/removable.
- Consider widening `isLocalHostname` to cover `.local`/`.internal`/`.corp`/`.lan` behind a flag once user signal justifies it; today they fall through to the probe + prompt sequence (NC-009).
