---
type: feature-tasks
title: "dev generate: Typed Dashboard and Alert Rule Stubs"
status: draft
spec: spec/feature-dev-generate-typed-stubs/spec.md
plan: spec/feature-dev-generate-typed-stubs/plan.md
created: 2026-03-09
---

# Implementation Tasks

## Dependency Graph

```
T1 (templates) --+
                  +---> T3 (generate.go command) ---> T4 (tests)
T2 (command.go) -+
```

## Wave 1: Templates and Registration

### T1: Create generate templates

**Priority**: P0
**Effort**: Small
**Depends on**: none
**Type**: task

Create the two Go template files that produce dashboard and alertrule stubs. The dashboard template MUST use `dashboardv2beta1.Manifest()` with a sample timeseries panel and AutoGridLayout. The alertrule template MUST use `resource.NewManifestBuilder()` with placeholder Condition/For/Labels/Annotations. Both templates MUST produce gofmt-valid Go code with correct imports and no trailing whitespace.

**Deliverables:**
- `cmd/grafanactl/dev/templates/generate/dashboard.go.tmpl`
- `cmd/grafanactl/dev/templates/generate/alertrule.go.tmpl`

**Acceptance criteria:**
- GIVEN the dashboard template is rendered with `Package=dashboards`, `FuncName=MyServiceOverview`, `Name=my-service-overview`
  WHEN the output is parsed by `go/parser.ParseFile`
  THEN no parse errors are returned (traces to spec AC: dashboard go/parser validity)

- GIVEN the alertrule template is rendered with `Package=alerts`, `FuncName=HighCpuUsage`, `Name=high-cpu-usage`
  WHEN the output is parsed by `go/parser.ParseFile`
  THEN no parse errors are returned (traces to spec AC: alertrule go/parser validity)

- GIVEN the dashboard template output
  WHEN inspected
  THEN it contains imports for `dashboardv2beta1`, `timeseries`, `testdata`, and `resource` packages; a function returning `*resource.ManifestBuilder`; `NewDashboardBuilder` with the resource name; a sample panel; `AutoGridLayout`; and `Manifest()` call (traces to FR-008)

- GIVEN the alertrule template output
  WHEN inspected
  THEN it contains imports for `alerting` and `resource` packages; a function returning `*resource.ManifestBuilder`; `NewRuleBuilder` with the resource name; `Condition("A")`, `For("5m")`, `Labels`, `Annotations` placeholders; and `resource.NewManifestBuilder()` wrapping (traces to FR-009)

---

### T2: Update command.go embed directive and register generate subcommand

**Priority**: P0
**Effort**: Small
**Depends on**: none
**Type**: task

Update the `//go:embed` directive in `command.go` to include `templates/generate/*.tmpl`. Add `cmd.AddCommand(generateCmd())` to the `Command()` function. This task creates the minimal wiring so T3 can define `generateCmd()` without circular issues.

**Deliverables:**
- `cmd/grafanactl/dev/command.go` (modified)

**Acceptance criteria:**
- GIVEN the updated `command.go`
  WHEN the embed directive is inspected
  THEN it includes `templates/generate/*.tmpl` (traces to FR-017)

- GIVEN the updated `command.go`
  WHEN the `Command()` function is inspected
  THEN it calls `cmd.AddCommand(generateCmd())` (traces to FR-015)

---

## Wave 2: Core Command Implementation

### T3: Implement generate.go with generateCmd, type inference, and file writing

**Priority**: P0
**Effort**: Medium
**Depends on**: T1, T2
**Type**: task

Implement `generate.go` in `cmd/grafanactl/dev/` containing:
1. `generateOpts` struct with `Type string` field and `setup(flags)` method binding `--type`/`-t` (FR-016)
2. `generateCmd()` returning `*cobra.Command` with `Use: "generate [FILE_PATH]..."`, `Args: cobra.MinimumNArgs(1)` (FR-001, FR-002, FR-007)
3. Type inference map: `dashboards`/`dashboard` → `dashboard`, `alerts`/`alertrules`/`alertrule` → `alertrule` (case-insensitive, immediate parent directory only) (FR-003)
4. Name inference: strip `.go` extension from filename (FR-004)
5. `--type` flag override (FR-005), error on unknown type without `--type` (FR-006)
6. File-exists check with descriptive error (negative constraint: no overwrite)
7. Output path normalization: `xstrings.ToSnakeCase` + `.go` (FR-010)
8. Directory creation via `ensureDirectory` (FR-011)
9. Template selection and execution based on resolved type
10. Per-arg success/error reporting via `cmdio.Success`/`cmdio.Error` (FR-012)
11. Summary line after all args processed (FR-013)

**Deliverables:**
- `cmd/grafanactl/dev/generate.go`

**Acceptance criteria:**
- GIVEN the user runs `grafanactl dev generate dashboards/my-service-overview.go`
  WHEN the command completes
  THEN a file `dashboards/my_service_overview.go` is created containing function `MyServiceOverview()` returning `*resource.ManifestBuilder` using `dashboardv2beta1.NewDashboardBuilder("my-service-overview")` (traces to spec AC 1)

- GIVEN the user runs `grafanactl dev generate alerts/high-cpu-usage.go`
  WHEN the command completes
  THEN a file `alerts/high_cpu_usage.go` is created containing function `HighCpuUsage()` returning `*resource.ManifestBuilder` using `alerting.NewRuleBuilder("high-cpu-usage")` (traces to spec AC 3)

- GIVEN the user runs `grafanactl dev generate dashboards/my-dashboard`
  WHEN the filename has no `.go` extension
  THEN the output file is written to `dashboards/my_dashboard.go` and the resource name is `my-dashboard` (traces to spec AC 4)

- GIVEN the user runs `grafanactl dev generate internal/monitoring/cpu-alert.go --type alertrule`
  WHEN the directory `monitoring` does not match any known type
  THEN the `--type` flag overrides inference and an alertrule stub is generated at `internal/monitoring/cpu_alert.go` with package name `monitoring` (traces to spec AC 5)

- GIVEN the user runs `grafanactl dev generate custom/my-thing.go`
  WHEN the directory `custom` does not match a known type and `--type` is not provided
  THEN the command returns an error listing supported directories and suggesting `--type` (traces to spec AC 6)

- GIVEN the user runs `grafanactl dev generate` with no positional arguments
  WHEN the command validates arguments
  THEN the command returns an error indicating at least one file path is required (traces to spec AC 7)

- GIVEN the user runs `grafanactl dev generate dashboards/a.go dashboards/b.go alerts/c.go`
  WHEN the command completes
  THEN three files are generated with a summary reporting 3 files generated (traces to spec AC 8)

- GIVEN the output directory does not exist
  WHEN generating a file at `new/nested/dashboards/test.go`
  THEN the directory `new/nested/dashboards` is created before writing (traces to spec AC 9)

- GIVEN the user runs `grafanactl dev generate dashboards/my-dashboard.go`
  WHEN the command completes successfully
  THEN the success message printed to stdout contains the output file path `dashboards/my_dashboard.go` (traces to spec AC 10)

- GIVEN the file `dashboards/existing.go` already exists
  WHEN the user runs `grafanactl dev generate dashboards/existing.go`
  THEN the command returns an error indicating the file exists (traces to spec AC 13)

---

## Wave 3: Tests

### T4: Add unit tests for generate command

**Priority**: P0
**Effort**: Medium
**Depends on**: T3
**Type**: task

Add `generate_test.go` with table-driven tests covering:
1. Type inference from directory names (all mappings including case-insensitive variants)
2. Name inference with and without `.go` extension
3. `--type` flag override
4. Error case: unknown directory without `--type`
5. Error case: file already exists
6. Dashboard template output parsed by `go/parser.ParseFile` with no errors
7. Alertrule template output parsed by `go/parser.ParseFile` with no errors
8. Dashboard template output matches `go/format.Source` (gofmt-valid)
9. Alertrule template output matches `go/format.Source` (gofmt-valid)
10. Batch generation produces correct file count
11. Package name derived from immediate parent directory

All tests MUST use `t.TempDir()` for isolation.

**Deliverables:**
- `cmd/grafanactl/dev/generate_test.go`

**Acceptance criteria:**
- GIVEN a generated dashboard Go file
  WHEN parsed by `go/parser.ParseFile`
  THEN no parse errors are returned (traces to spec AC 11)

- GIVEN a generated alertrule Go file
  WHEN parsed by `go/parser.ParseFile`
  THEN no parse errors are returned (traces to spec AC 12)

- GIVEN the test suite runs via `make tests`
  WHEN all generate tests execute
  THEN all tests pass with zero failures

- GIVEN each type inference mapping (`dashboards`, `dashboard`, `alerts`, `alertrules`, `alertrule`, `Dashboards`, `ALERTS`)
  WHEN the inference function is called
  THEN the correct type string is returned (traces to FR-003)

- GIVEN a filename with `.go` extension and a filename without extension
  WHEN the name inference function is called
  THEN both produce the correct resource name (traces to FR-004)
