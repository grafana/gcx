---
type: feature-tasks
title: "Login & Config Consolidation"
status: draft
spec: docs/specs/feature-login-config-consolidation/spec.md
plan: docs/specs/feature-login-config-consolidation/plan.md
generated: 2026-04-20
---

# Implementation Tasks

## Dependency Graph

```
T1 (AuthMethod config field)   T2 (internal/login scaffold)
         |                              |
         |                    ┌─────────┴─────────┐
         |                    v                   v
         |               T3 (detect.go)     T4 (validate.go)
         |                    |                   |
         └────────────────────┴────────┬──────────┘
                                       v
                                  T5 (login.Run())
                                       |
                    ┌──────────────────┼──────────────────┐
                    v                  v                   v
            T6 (cmd/gcx/login)   T7 (delete auth cmd)  T8 (config check)
```

## Wave 1: Foundation

### T1: Add AuthMethod field to GrafanaConfig

**Priority**: P1
**Effort**: Small
**Depends on**: none
**Type**: task

Add the new `AuthMethod string` field to `GrafanaConfig` in `internal/config/types.go`
with proper json/yaml tags (`omitempty`), backward-compatible loading, and inference
logic needed by `gcx config check` (FR-012, FR-017, NC-003, D13).

**Deliverables:**
- `internal/config/types.go` — `GrafanaConfig.AuthMethod` field added with `json:"auth-method,omitempty" yaml:"auth-method,omitempty"`
- Optionally: a helper method `GrafanaConfig.InferredAuthMethod() string` returning `"oauth"`, `"token"`, `"basic"`, or `"unknown"` by inspecting populated fields — this is needed by both login.Run() and config check

**Acceptance criteria:**
- GIVEN a config file written with `auth-method: oauth` WHEN loaded THEN `GrafanaConfig.AuthMethod == "oauth"`
- GIVEN a legacy config file without any `auth-method` field WHEN loaded THEN `GrafanaConfig.AuthMethod == ""` and deserialization succeeds (AC-012)
- GIVEN a `GrafanaConfig` with `OAuthToken` set and empty `AuthMethod` WHEN `InferredAuthMethod()` is called THEN it returns `"oauth"`
- GIVEN a `GrafanaConfig` with `APIToken` set and empty `AuthMethod` WHEN `InferredAuthMethod()` is called THEN it returns `"token"`

---

### T2: Scaffold internal/login package

**Priority**: P0
**Effort**: Small
**Depends on**: none
**Type**: task

Create the `internal/login` package skeleton: all shared types (`Options`, `Result`,
`Target`), sentinel errors (`ErrNeedInput`, `ErrNeedClarification`), and the `AuthFlow`
interface. This provides the type foundation for T3, T4, and T5 (FR-001, FR-002, FR-003,
FR-004, FR-005, D1, D2, D3, D4, D6).

**Deliverables:**
- `internal/login/login.go` — package declaration, `Options`, `Result`, `Target` type + constants (`TargetUnknown`, `TargetCloud`, `TargetOnPrem`), `ErrNeedInput`, `ErrNeedClarification`, `AuthFlow` interface stub, `Run()` function signature only (returns `nil, nil` — body filled in T5)
- No test file yet (tests go in T5)

**Acceptance criteria:**
- GIVEN a test importing `internal/login` WHEN it constructs `login.Options{}` and `login.Result{}` THEN it compiles with all required fields present (FR-004, FR-005)
- GIVEN a call to `login.Run(ctx, opts)` THEN it compiles and returns `(Result{}, nil)` as a stub
- GIVEN `internal/login/login.go` THEN it has zero imports of `cmd/`, `huh`, or `os.Stdin` access (NC-001)

---

## Wave 2: Core Logic

### T3: Implement target detection (detect.go)

**Priority**: P1
**Effort**: Medium
**Depends on**: T2
**Type**: task

Implement `internal/login/detect.go` containing all target-detection logic: known
Grafana Cloud domain matching via `stackSlugFromServerURL`, local-hostname detection,
unauthenticated `/api/frontend/settings` probe with ≤3s timeout, and the Unknown
fallback (FR-006, FR-007, FR-008, FR-025, NC-009, D5, D10, D21).

Note: `stackSlugFromServerURL` already exists in `internal/config` — call it rather than
re-implement.

**Deliverables:**
- `internal/login/detect.go` — exported `DetectTarget(ctx, server string, httpClient) (Target, error)` and `isLocalHostname(host string) bool` (package-internal), probe logic for `/api/frontend/settings`

**Acceptance criteria:**
- GIVEN `https://example.grafana.net` WHEN `DetectTarget` is called THEN returns `TargetCloud` (AC-014)
- GIVEN `http://localhost:3000` WHEN `DetectTarget` is called THEN returns `TargetOnPrem` (AC-003)
- GIVEN `https://grafana.example.com` and a probe that times out WHEN `DetectTarget` is called THEN returns `TargetUnknown` (AC-019)
- GIVEN a URL with `.local`, `.internal`, `.corp`, or `.lan` suffix WHEN `DetectTarget` is called THEN returns `TargetUnknown`, not `TargetOnPrem` (NC-009)
- GIVEN `https://grafana.example.com` where the probe returns Cloud-specific markers WHEN `DetectTarget` is called THEN returns `TargetCloud` without prompting (AC-018)

---

### T4: Implement connectivity validation (validate.go)

**Priority**: P1
**Effort**: Medium
**Depends on**: T2
**Type**: task

Implement `internal/login/validate.go` containing the full validation pipeline:
`/api/health`, K8s discovery registry `/apis`, `grafana.GetVersion() >= 12`, and
GCOM `GetStack(slug)` when Cloud+CAP token is present (FR-013, FR-014, D11, D12, NC-002,
NC-005, NC-010).

Use existing clients: `internal/grafana.Client`, `internal/cloud.GCOMClient`,
`internal/resources/discovery.NewDefaultRegistry`. No new raw `http.Client`.

**Deliverables:**
- `internal/login/validate.go` — `validate(ctx, opts Options, grafana, cloud, discovery clients) error` function; all validation steps in order; returns typed error naming the failed step; no config write on any failure path

**Acceptance criteria:**
- GIVEN a server responding to `/api/health` but returning Grafana version 11 WHEN `validate` runs THEN returns an error naming the version check (AC-013)
- GIVEN all checks passing WHEN `validate` runs THEN returns nil and no config mutation occurs inside this function
- GIVEN `Target == TargetCloud` and a non-empty `CloudToken` WHEN `validate` runs THEN `GCOMClient.GetStack` is called
- GIVEN `Target == TargetOnPrem` WHEN `validate` runs THEN GCOM check is skipped entirely

---

## Wave 3: Orchestration

### T5: Implement login.Run() and table-driven tests

**Priority**: P0
**Effort**: Large
**Depends on**: T1, T2, T3, T4
**Type**: task

Wire up the full `login.Run(ctx, Options) (Result, error)` orchestration following the
8-step flow in plan.md: server URL resolution, context name derivation, target detection,
Step 1 Grafana auth, Step 2 Cloud API auth, connectivity validation, config persistence,
and Result construction (FR-009, FR-010, FR-011, FR-012, FR-015, FR-016, D7, D8, D9,
D12, D14, D20, NC-002, NC-004, NC-010).

Also add `internal/login/login_test.go` with table-driven tests covering the
representative cases from plan.md Testing Strategy section.

**Deliverables:**
- `internal/login/login.go` — full `Run()` body replacing the stub from T2
- `internal/login/login_test.go` — table-driven tests: first-run Cloud with CAP (AC-001 unit proxy), first-run Cloud no CAP (AC-002), on-prem with SA token (AC-003), `--yes` defaults on-prem (AC-005), agent mode structured error (AC-008), validation failure leaves CurrentContext untouched (AC-013), AuthMethod roundtrip (AC-009, AC-011), legacy config no AuthMethod (AC-012)

**Acceptance criteria:**
- GIVEN `login_test.go` imports `internal/login` with injected stubs WHEN tests run THEN `go test ./internal/login/...` passes with no stdin reads or huh imports (AC-015)
- GIVEN validation passes WHEN `Run()` completes THEN the named context is written with `AuthMethod` set and `CurrentContext` updated (FR-016, AC-011)
- GIVEN validation fails WHEN `Run()` returns THEN the config file is not modified and `CurrentContext` is unchanged (AC-013, NC-002, NC-010)
- GIVEN `opts.Yes = true` or agent mode and missing server WHEN `Run()` is called THEN returns `ErrNeedInput{Fields: ["server"]}` and no huh form is triggered (AC-008, NC-007)

---

## Wave 4: CLI Integration

### T6: Implement cmd/gcx/login command

**Priority**: P1
**Effort**: Medium
**Depends on**: T5
**Type**: task

Create `cmd/gcx/login/command.go` — thin Cobra wiring that registers flags, builds
`login.Options` from flags/env/config, runs the `huh` interactive forms for missing
inputs, drives the `login.Run()` retry loop on sentinel errors, and translates `Result`
into user-facing output and exit status (FR-003, FR-021, FR-022, FR-023, D16, D17,
NC-007, AC-001–AC-009).

Register the new command in the root Cobra command (wherever `cmd/gcx/auth` is currently
registered, add `cmd/gcx/login` alongside or replace it — T7 handles the auth removal).

Pattern reference: `cmd/gcx/dev/scaffold.go` `askMissingOpts` for huh form construction.

**Deliverables:**
- `cmd/gcx/login/command.go` — full Cobra command; flag registration (`--server`, `--token`, `--cloud-token`, `--cloud`, `--context`, `--yes`, `--cloud-api-url`); `huh` Form 1 (server) and Form 2 (target clarification, auth method, CAP token); retry loop handling `ErrNeedInput` / `ErrNeedClarification`; `--yes`/agent-mode path that surfaces structured error
- `cmd/root/root.go` (or equivalent) — `login.Command()` registered

**Acceptance criteria:**
- GIVEN interactive TTY and a `.grafana.net` URL WHEN `gcx login` runs THEN no target-clarification prompt appears and two-step auth proceeds (AC-014 CLI check)
- GIVEN `--yes --server <url> --token <tok>` and a localhost URL WHEN `gcx login` runs THEN exits 0, no huh form shown (AC-003, AC-005)
- GIVEN agent mode and no `--server` flag WHEN `gcx login` runs THEN exits non-zero with structured error naming missing fields (AC-008, NC-007)
- GIVEN `gcx login --help` THEN shows flag docs including `--cloud`, `--cloud-token`, `--yes` flags

---

### T7: Remove cmd/gcx/auth package

**Priority**: P1
**Effort**: Small
**Depends on**: T5
**Type**: chore

Hard-remove `cmd/gcx/auth/` package and unregister the `auth` subcommand from the root
Cobra command. No compatibility shim (FR-018, FR-019, NC-008, AC-010, D15).

**Deliverables:**
- `cmd/gcx/auth/` directory deleted entirely
- Root command registration of `auth.Command()` removed
- Any import of `cmd/gcx/auth` removed from all files
- `go build ./...` passes after deletion

**Acceptance criteria:**
- GIVEN invoking `gcx auth` or `gcx auth login` WHEN the CLI parses THEN exits non-zero with Cobra's default "unknown command" error and no config mutation (AC-010)
- GIVEN `go build ./...` THEN compilation succeeds with zero references to the deleted package

---

### T8: Update gcx config check for auth-method

**Priority**: P2
**Effort**: Small
**Depends on**: T5
**Type**: task

Update `gcx config check` to display an `auth-method` row for the active context.
Show the stored value verbatim when non-empty; otherwise infer from populated auth fields
using `GrafanaConfig.InferredAuthMethod()` from T1 (FR-024, AC-016, AC-017, D22).

**Deliverables:**
- `cmd/gcx/config/command.go` (or the check subcommand file) — `auth-method` row added to the check output table
- Tests or manual verification of both verbatim and inferred display paths

**Acceptance criteria:**
- GIVEN an active context with `GrafanaConfig.AuthMethod == "oauth"` WHEN `gcx config check` runs THEN output contains `auth-method: oauth` (AC-016)
- GIVEN an active context with empty `AuthMethod` but `APIToken` set WHEN `gcx config check` runs THEN output contains `auth-method: token` (inferred) (AC-017)
- GIVEN an active context with no auth fields at all WHEN `gcx config check` runs THEN output contains `auth-method: unknown` (inferred fallback)
