---
type: feature-spec
title: "dev generate: Typed Dashboard and Alert Rule Stubs"
status: done
beads_id: grafanactl-experiments-cyi
created: 2026-03-09
---

# dev generate: Typed Dashboard and Alert Rule Stubs

## Problem Statement

Developers using grafanactl's "as code" workflow must manually write Go files that use grafana-foundation-sdk builder types to define dashboards and alert rules from scratch. This is error-prone and slow because:

1. The foundation-sdk builder API surface is large (dozens of builder methods per type), and discovering the correct import paths and method chains requires reading SDK source code.
2. There is no command that generates a ready-to-edit, compilable Go stub using the builder pattern. `dev scaffold` generates a full project with a single sample dashboard, but does not support generating individual resource files for dashboards or alert rules. `dev import` converts existing remote resources into builder code, but cannot create new resources from scratch.

**Who is affected:** Developers building Grafana resources-as-code projects who want to add new dashboards or alert rules without copy-pasting from existing files.

**Current workaround:** Copy the scaffold sample (`sample.go.tmpl`) or an imported file and manually edit it, or write builder code from scratch by consulting the SDK documentation.

## Scope

### In Scope

- A new `dev generate` subcommand under the existing `dev` command group
- Positional file path arguments that encode both resource type (from directory) and resource name (from filename), following the `resources pull` UX pattern
- Generation of typed Go stubs for **Dashboard** resources (v2beta1 API version)
- Generation of typed Go stubs for **AlertRule** resources using the `alerting` foundation-sdk package
- Generated stubs use the foundation-sdk builder pattern (`NewDashboardBuilder`, `NewRuleBuilder`, etc.)
- Generated stubs wrap the builder result in a `resource.ManifestBuilder` for grafanactl compatibility
- Type inference from directory name (e.g., `dashboards/` maps to dashboard type)
- Name inference from filename (e.g., `my-dashboard.go` maps to name `my-dashboard`)
- Optional `--type` flag as fallback when the directory name does not imply a known resource type
- Support for multiple positional arguments for batch generation
- Template-based generation using the existing `text/template` + `embed.FS` approach
- Generated files compile without modification (valid Go code with correct imports)

### Out of Scope

- **Interactive form-based generation** (like `dev scaffold` uses `huh`). The generate command takes all inputs via flags/arguments. Rationale: stubs are simple, single-file outputs; interactive prompts add unnecessary friction.
- **Generation for resource types other than Dashboard and AlertRule.** Rationale: these are the two primary resource types with foundation-sdk builder support. Other types (folders, playlists, contact points, notification policies) can be added incrementally.
- **Generating multi-file structures or project scaffolds.** Rationale: `dev scaffold` already handles project-level generation. `dev generate` produces single resource files.
- **Dashboard v0alpha1 or v1beta1 stubs.** Rationale: v2beta1 is the current recommended API version. Import already handles legacy versions for conversion, but new stubs MUST target the latest version only.
- **Running `goimports` or `gofmt` on generated output.** Rationale: templates MUST produce correctly formatted code; post-processing adds a runtime dependency.
- **Generating query/datasource stubs inside alert rules.** The alert rule stub includes a placeholder query structure using the SDK's `NewQueryBuilder`, but does not generate datasource-specific query types (Prometheus, Loki, etc.).

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Command name | `dev generate` | Consistent with "generate" semantics (creating new code). Distinct from `import` (which converts existing remote resources) and `scaffold` (which creates project structure). | Codebase convention |
| Interface style | Positional file path(s): `dev generate dashboards/my-dashboard.go` | Mirrors `resources pull dashboards/my-dashboard` UX. Type and name are encoded in the path, eliminating the need for `--name` and `--path` flags. Consistent with the project's convention of using positional selectors. | User feedback; `resources pull` pattern |
| Type inference from directory | Map directory name to resource type: `dashboards` → dashboard, `alerts`/`alertrules` → alertrule | Eliminates explicit type argument. Directory name doubles as the output directory, keeping generated files organized by type — matching the layout that `resources pull` produces. | `resources pull` convention; `local.GroupResourcesByKind` |
| Name inference from filename | Strip `.go` extension and use the stem as the resource name: `my-dashboard.go` → name `my-dashboard` | The filename is the natural place to encode the resource name. The `.go` extension is optional — bare names work too. | `dev import` convention (`xstrings.ToSnakeCase` for filenames) |
| `--type` flag as fallback | Optional `--type` flag overrides directory-based inference | Handles cases where the directory name does not match a known type (e.g., `internal/monitoring/cpu-alert.go`). | Ergonomic fallback |
| Batch generation | Multiple positional args: `dev generate dashboards/a.go dashboards/b.go alerts/c.go` | Follows `resources pull` pattern of accepting multiple selectors. Each arg is processed independently. | `resources pull` pattern |
| Dashboard API version | v2beta1 only | Latest version, used by scaffold sample. New stubs MUST target current API. | Existing scaffold template uses v2beta1 |
| Alert rule builder source | `alerting.NewRuleBuilder` + `alerting.NewRuleGroupBuilder` from foundation-sdk | Confirmed available in SDK v0.0.12 with full builder API (Condition, Queries, Labels, Annotations, etc.) | SDK source inspection |
| Manifest wrapping for alerts | `resource.NewManifestBuilder()` (manual) | Unlike `dashboardv2beta1.Manifest()` convenience function, the alerting package has no `Manifest()` helper. Generated code MUST use `resource.NewManifestBuilder()` directly. | SDK source inspection |
| Output file naming | Normalize filename to snake_case: `my-dashboard.go` is written as `my_dashboard.go` | Matches existing `dev import` convention (`xstrings.ToSnakeCase`). The resource name retains the original kebab-case value (e.g., `my-dashboard`). | `import.go` line 116 |
| Package name derivation | Derived from the immediate parent directory of the output file | Matches Go convention; consistent with import command behavior. | `import.go` line 129 |
| Template location | `templates/generate/dashboard.go.tmpl`, `templates/generate/alertrule.go.tmpl` | Follows existing pattern of `templates/<subcommand>/*.tmpl` | Existing template organization |

## Functional Requirements

**FR-001:** The system MUST register a `generate` subcommand under the `dev` command group, accessible as `grafanactl dev generate`.

**FR-002:** The `generate` command MUST accept one or more positional file path arguments. Each argument specifies where to write the generated stub and encodes both the resource type and name.

**FR-003:** The system MUST infer the resource type from the directory component of each positional path using this mapping:

| Directory name | Inferred type |
|----------------|---------------|
| `dashboards`   | dashboard     |
| `alerts`       | alertrule     |
| `alertrules`   | alertrule     |

Directory matching MUST be case-insensitive and MUST use only the immediate parent directory of the file (e.g., `internal/dashboards/foo.go` uses `dashboards` for inference, not `internal`).

**FR-004:** The system MUST infer the resource name from the filename component of each positional path by stripping the `.go` extension (if present). For example, `my-dashboard.go` yields the name `my-dashboard`; `my-dashboard` also yields `my-dashboard`.

**FR-005:** The `generate` command MUST accept an optional `--type` flag (short: `-t`) that overrides directory-based type inference for all positional arguments. Supported values: `dashboard`, `alertrule`.

**FR-006:** If the directory name does not match a known type (per FR-003) and `--type` is not provided, the command MUST return an error listing the supported types and suggesting the `--type` flag.

**FR-007:** The command MUST require at least one positional argument. If none are provided, the command MUST return an error with a usage hint showing the expected format.

**FR-008:** When the inferred or explicit type is `dashboard`, the system MUST generate a Go file containing:
- A package declaration derived from the immediate parent directory name (lowercased)
- Import statements for `dashboardv2beta1`, `timeseries`, `testdata`, and `resource` packages from grafana-foundation-sdk
- A function named after the resource name (CamelCase via `xstrings.ToCamelCase`) that returns `*resource.ManifestBuilder`
- A `dashboardv2beta1.NewDashboardBuilder()` call with the resource name as the dashboard title
- A sample panel using `timeseries.NewVisualizationBuilder()` with a `testdata` query
- An `AutoGridLayout` with the sample panel
- A `dashboardv2beta1.Manifest()` call wrapping the builder

**FR-009:** When the inferred or explicit type is `alertrule`, the system MUST generate a Go file containing:
- A package declaration derived from the immediate parent directory name (lowercased)
- Import statements for `alerting` and `resource` packages from grafana-foundation-sdk
- A function named after the resource name (CamelCase via `xstrings.ToCamelCase`) that returns `*resource.ManifestBuilder`
- An `alerting.NewRuleBuilder()` call with the resource name as the rule title
- Placeholder builder calls for `Condition("A")`, `For("5m")`, `Labels(map[string]string{})`, and `Annotations(map[string]string{"summary": ""})` as editable scaffolding
- A `resource.NewManifestBuilder()` wrapping the rule with `ApiVersion`, `Kind`, and `Metadata`

**FR-010:** The output file path MUST normalize the filename to snake_case using `xstrings.ToSnakeCase` and ensure the `.go` extension is present. For example, input `dashboards/my-dashboard.go` writes to `dashboards/my_dashboard.go`; input `dashboards/my-dashboard` writes to `dashboards/my_dashboard.go`.

**FR-011:** The system MUST create intermediate directories as needed (using the existing `ensureDirectory` function) before writing the output file.

**FR-012:** When processing multiple positional arguments, the system MUST process each independently. A failure on one file MUST NOT prevent generation of the remaining files. Each success and failure MUST be reported individually via `cmdio.Success` and `cmdio.Error` respectively.

**FR-013:** After all files are processed, the system MUST print a summary indicating how many files were generated and how many failed.

**FR-014:** The generated Go code MUST be syntactically valid (parseable by `go/parser`) and correctly formatted (matching `gofmt` output).

**FR-015:** The `generate` subcommand MUST be added to the `dev` command group in `command.go` via `cmd.AddCommand()`.

**FR-016:** The `generate` command MUST follow the Options pattern: a `generateOpts` struct with `setup(flags)` method, consistent with `importOpts` and `scaffoldOpts`.

**FR-017:** The `//go:embed` directive in `command.go` MUST be updated to include `templates/generate/*.tmpl`.

## Acceptance Criteria

- GIVEN the user runs `grafanactl dev generate dashboards/my-service-overview.go`
  WHEN the command completes
  THEN a file `dashboards/my_service_overview.go` is created containing a function `MyServiceOverview()` that returns `*resource.ManifestBuilder` using `dashboardv2beta1.NewDashboardBuilder("my-service-overview")`

- GIVEN the user runs `grafanactl dev generate dashboards/my-service-overview.go`
  WHEN the command completes
  THEN the generated file compiles successfully as part of a Go module that imports grafana-foundation-sdk v0.0.12

- GIVEN the user runs `grafanactl dev generate alerts/high-cpu-usage.go`
  WHEN the command completes
  THEN a file `alerts/high_cpu_usage.go` is created containing a function `HighCpuUsage()` that returns `*resource.ManifestBuilder` using `alerting.NewRuleBuilder("high-cpu-usage")`

- GIVEN the user runs `grafanactl dev generate dashboards/my-dashboard`
  WHEN the filename has no `.go` extension
  THEN the output file is written to `dashboards/my_dashboard.go` and the resource name is inferred as `my-dashboard`

- GIVEN the user runs `grafanactl dev generate internal/monitoring/cpu-alert.go --type alertrule`
  WHEN the directory `monitoring` does not match any known type
  THEN the `--type` flag overrides inference and an alertrule stub is generated at `internal/monitoring/cpu_alert.go` with package name `monitoring`

- GIVEN the user runs `grafanactl dev generate custom/my-thing.go`
  WHEN the directory `custom` does not match any known type and `--type` is not provided
  THEN the command returns an error stating that resource type cannot be inferred from directory `custom`, listing supported directory names (`dashboards`, `alerts`, `alertrules`), and suggesting `--type`

- GIVEN the user runs `grafanactl dev generate` with no positional arguments
  WHEN the command validates arguments
  THEN the command returns an error indicating that at least one file path argument is required

- GIVEN the user runs `grafanactl dev generate dashboards/a.go dashboards/b.go alerts/c.go`
  WHEN the command completes
  THEN three files are generated: `dashboards/a.go`, `dashboards/b.go`, and `alerts/c.go`, with a summary reporting 3 files generated

- GIVEN the output directory does not exist
  WHEN the user runs `grafanactl dev generate new/nested/dashboards/test.go`
  THEN the directory `new/nested/dashboards` is created with mode 0744 before writing the file, and the type `dashboard` is inferred from the immediate parent directory `dashboards`

- GIVEN the user runs `grafanactl dev generate dashboards/my-dashboard.go`
  WHEN the command completes successfully
  THEN a success message is printed to stdout containing the output file path `dashboards/my_dashboard.go`

- GIVEN a generated dashboard Go file
  WHEN parsed by `go/parser.ParseFile`
  THEN no parse errors are returned

- GIVEN a generated alertrule Go file
  WHEN parsed by `go/parser.ParseFile`
  THEN no parse errors are returned

- GIVEN the user runs `grafanactl dev generate dashboards/existing.go`
  WHEN the file `dashboards/existing.go` already exists
  THEN the command returns an error indicating the file already exists and suggesting the user delete it first or use a different name

## Negative Constraints

- The `generate` command MUST NOT require a connection to a Grafana instance (no REST config, no API calls). It is purely a local code generation tool.

- The `generate` command MUST NOT overwrite an existing file at the target path without producing an error. If the file already exists, the command MUST return an error with a message indicating the file already exists and suggesting the user delete it first or use a different name.

- The generated code MUST NOT use the converter pattern (`DashboardConverter`, `dashboardv2Converter`). Converters are for `dev import` (transforming existing resources). Generated stubs MUST use the builder pattern exclusively.

- The generated code MUST NOT import packages that are not part of grafana-foundation-sdk. The stub MUST be self-contained within the SDK's type system.

- The `generate` command MUST NOT use interactive prompts (no `huh` forms). All inputs MUST be provided via flags and arguments.

- The `generate` command MUST NOT accept `--name` or `--path` flags. Name and path are encoded in the positional argument.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Foundation-sdk alerting builder API changes in future versions | Generated alert stubs may not compile against newer SDK versions | Pin generated import to the SDK package path without version suffix; document SDK version compatibility in command help text |
| Template formatting drifts from gofmt standard | Generated code requires manual formatting | Add a unit test that runs `go/parser.ParseFile` and `go/format.Source` on template output to verify syntax and formatting |
| Resource name edge cases (special characters, Go keyword conflicts) | Generated function names or file names may be invalid | Validate resource name: MUST match `^[a-z][a-z0-9-]*$`; reject names that produce Go keyword collisions |
| Users expect generate to update an `All()` registry function | Generated file is not automatically registered in an aggregator | Document in command output that the user must manually add the function call to their registry (e.g., `All()` function) |
| Directory name ambiguity (e.g., `dashboard` singular vs `dashboards` plural) | Type inference fails on valid-seeming paths | Map both singular and plural forms in FR-003; document the supported directory names in `--help` output |
| Deeply nested paths obscure the type-bearing directory | User expects `a/b/dashboards/c/foo.go` to infer type from `dashboards`, but FR-003 uses only the immediate parent `c` | FR-003 specifies immediate parent only; `--type` flag handles non-matching directories. Document this in `--help` with examples. |

## Open Questions

- [RESOLVED] Does the alerting package in foundation-sdk v0.0.12 have builder types? **Yes.** `NewRuleBuilder(title)` and `NewRuleGroupBuilder(title)` are available with full builder API including Condition, Queries, Labels, Annotations, For, ExecErrState, NoDataState, FolderUID, NotificationSettings.

- [RESOLVED] Does the alerting package have a `Manifest()` convenience function like dashboardv2beta1? **No.** The generated alert stub MUST use `resource.NewManifestBuilder()` directly.

- [RESOLVED] Should the command use `--name`/`--path` flags or positional file paths? **Positional file paths.** The path encodes both type (from directory) and name (from filename), following the `resources pull` UX pattern of `type/name` selectors.

- [DEFERRED] Should `dev generate` support generating notification policy or contact point stubs? These have builders in the SDK (`NewContactPointBuilder`, `NewNotificationPolicyBuilder`). Deferred to a follow-up spec to keep initial scope focused.

- [DEFERRED] Should generated stubs include a corresponding `_test.go` file with a build-validation test? Useful for CI but adds complexity. Deferred to follow-up.

- [DEFERRED] Should type inference also search ancestor directories (not just the immediate parent) for a matching type name? This would support paths like `internal/dashboards/subsystem/foo.go`. Deferred — `--type` flag handles this case adequately for now.
