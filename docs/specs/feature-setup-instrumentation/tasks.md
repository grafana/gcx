---
type: feature-tasks
title: "Declarative Instrumentation Setup under gcx setup"
status: draft
spec: docs/specs/feature-setup-instrumentation/spec.md
plan: docs/specs/feature-setup-instrumentation/plan.md
created: 2026-03-30
---

# Implementation Tasks

## Dependency Graph

```
T1 (shared fleet client) ──┬──► T3 (instrumentation client + types)
                            │          │
                            │          ├──► T5 (status command)
                            │          ├──► T6 (discover command)
                            │          ├──► T7 (show command)
                            │          └──► T8 (apply command)
                            │                    │
T2 (pipeline protection) ──┼──────────────────► T9 (docs + verification)
                            │                    │
T4 (setup area wiring) ────┴──► T5,T6,T7,T8 ──► T9
```

## Wave 1: Foundation

### T1: Extract shared fleet client to `internal/fleet/`
**Priority**: P0
**Effort**: Medium
**Depends on**: none
**Type**: task

Extract the base HTTP client (`Client` struct, `NewClient`, `doRequest`, `readErrorBody`, auth logic) from `internal/providers/fleet/client.go` into a new `internal/fleet/` package. Add a `LoadClient(ctx)` helper in `internal/fleet/config.go` that encapsulates the `LoadCloudConfig → StackInfo → NewClient` pattern. Refactor `internal/providers/fleet/client.go` to import and delegate to `internal/fleet/`. Refactor `fleetHelper.loadClient`, `NewPipelineTypedCRUD`, and `NewCollectorTypedCRUD` to use `internal/fleet.LoadClient` or the extracted base client.

**Implements:** FR-030, FR-031, FR-032, FR-033, FR-034, FR-035

**Deliverables:**
- `internal/fleet/client.go` — `Client` struct, `NewClient`, `doRequest`, `readErrorBody`
- `internal/fleet/config.go` — `LoadClient(ctx, *providers.ConfigLoader) (*Client, string, error)`
- `internal/fleet/errors.go` — shared error helpers
- `internal/fleet/client_test.go` — unit tests for base client (auth headers, error handling, JSON marshaling)
- `internal/providers/fleet/client.go` — refactored to import `internal/fleet/`
- `internal/providers/fleet/provider.go` — `fleetHelper.loadClient` and `NewPipelineTypedCRUD`/`NewCollectorTypedCRUD` refactored

**Acceptance criteria:**
- GIVEN the `internal/fleet/` package exists WHEN `internal/providers/fleet/client.go` is inspected THEN it MUST import and delegate to `internal/fleet/` for base HTTP operations (doRequest, auth)
- GIVEN the `internal/fleet/` refactoring is complete WHEN `make tests` is executed THEN all existing fleet provider tests MUST pass without modification to test assertions
- GIVEN a `Client` constructed with `useBasicAuth=true` WHEN `doRequest` is called THEN the HTTP request MUST contain an `Authorization: Basic {base64({instanceID}:{apiToken})}` header
- GIVEN a `Client` constructed with `useBasicAuth=false` WHEN `doRequest` is called THEN the HTTP request MUST contain an `Authorization: Bearer {apiToken}` header

---

### T2: Fleet-managed pipeline protection guard
**Priority**: P1
**Effort**: Small
**Depends on**: none
**Type**: task

Add a protection guard to the fleet provider's `pipeline update` and `pipeline delete` commands that rejects operations on pipelines whose name starts with `beyla_k8s_appo11y_`. Add a `--force` flag to both commands that overrides the guard. The guard checks the pipeline name after fetching it from the API (for update/delete by ID, the pipeline must be fetched first to check the name).

**Implements:** FR-038, FR-039, FR-040

**Deliverables:**
- `internal/providers/fleet/provider.go` — modified `newPipelineUpdateCommand` and `newPipelineDeleteCommand`
- `internal/providers/fleet/provider_test.go` — unit tests for pipeline protection guard

**Acceptance criteria:**
- GIVEN a pipeline named `beyla_k8s_appo11y_prod-1` WHEN `gcx fleet pipelines update <id>` is executed without `--force` THEN the command MUST fail with an error directing users to `gcx setup instrumentation apply`
- GIVEN a pipeline named `beyla_k8s_appo11y_prod-1` WHEN `gcx fleet pipelines delete <id>` is executed without `--force` THEN the command MUST fail with an error directing users to `gcx setup instrumentation apply`
- GIVEN a pipeline named `beyla_k8s_appo11y_prod-1` WHEN `gcx fleet pipelines update <id> --force` is executed THEN the update MUST proceed normally
- GIVEN a pipeline named `beyla_k8s_appo11y_prod-1` WHEN `gcx fleet pipelines delete <id> --force` is executed THEN the delete MUST proceed normally
- GIVEN a pipeline named `my-custom-pipeline` WHEN `gcx fleet pipelines update <id>` is executed without `--force` THEN the update MUST proceed normally (no guard triggered)

---

### T3: InstrumentationConfig manifest types and instrumentation client
**Priority**: P0
**Effort**: Medium-Large
**Depends on**: T1
**Type**: task

Define the `InstrumentationConfig` manifest struct in `internal/setup/instrumentation/types.go` with `apiVersion: setup.grafana.app/v1alpha1`, `kind: InstrumentationConfig`, and all spec fields (metadata, spec.app with namespaces/apps, spec.k8s). Build the instrumentation HTTP client in `internal/setup/instrumentation/client.go` on top of `internal/fleet.Client`, adding methods for all 7 Connect endpoints: `GetAppInstrumentation`, `SetAppInstrumentation`, `GetK8SInstrumentation`, `SetK8SInstrumentation`, `SetupK8sDiscovery`, `RunK8sDiscovery`, `RunK8sMonitoring`. Implement the optimistic lock comparison function in `internal/setup/instrumentation/compare.go`.

**Implements:** FR-024, FR-025, FR-026, FR-027, FR-028, FR-029, FR-036, FR-037, FR-017 (comparison logic)

**Deliverables:**
- `internal/setup/instrumentation/types.go` — `InstrumentationConfig`, `InstrumentationSpec`, `AppSpec`, `K8sSpec`, `NamespaceConfig`, `AppConfig`, validation functions
- `internal/setup/instrumentation/client.go` — `InstrumentationClient` with all 7 endpoint methods
- `internal/setup/instrumentation/compare.go` — `Compare(local, remote) (*Diff, error)` returning remote-only items
- `internal/setup/instrumentation/types_test.go` — manifest validation tests (wrong apiVersion, missing name, valid manifest)
- `internal/setup/instrumentation/client_test.go` — HTTP mock tests for all 7 endpoints
- `internal/setup/instrumentation/compare_test.go` — comparison tests: empty remote, empty local, superset, subset, exact match, per-workload differences

**Acceptance criteria:**
- GIVEN an `InstrumentationConfig` manifest WHEN inspected THEN it MUST NOT contain any datasource URLs, instance IDs, API tokens, or stack-specific values
- GIVEN a struct with correct apiVersion and kind WHEN all fields are marshaled to YAML and JSON THEN every field MUST have both `yaml` and `json` struct tags
- GIVEN a manifest with `apiVersion: wrong/v1` WHEN validation is called THEN it MUST return a validation error
- GIVEN a manifest with missing `metadata.name` WHEN validation is called THEN it MUST return a validation error stating that `metadata.name` (cluster name) is required
- GIVEN a remote config with namespace `monitoring` not in local manifest WHEN `Compare(local, remote)` is called THEN it MUST return a diff listing `monitoring` as remote-only
- GIVEN a local manifest that is a superset of remote WHEN `Compare(local, remote)` is called THEN it MUST return an empty diff
- GIVEN the instrumentation client WHEN any endpoint method is called THEN the HTTP request MUST use POST with `Content-Type: application/json` and `Accept: application/json`

---

### T4: Setup command area wiring (`cmd/gcx/setup/`)
**Priority**: P0
**Effort**: Small
**Depends on**: none
**Type**: task

Create the `cmd/gcx/setup/` package with `command.go` that exposes `setup.Command() *cobra.Command`. Wire it into `root/command.go` via `rootCmd.AddCommand(setup.Command())`. The setup command has `Use: "setup"`, `Short: "Onboard and configure Grafana Cloud products."`. Add a `status` subcommand stub that will display aggregated product status (initially just instrumentation). Create the `cmd/gcx/setup/instrumentation/` sub-package with an `instrumentation` group command that will hold `status`, `discover`, `show`, `apply` subcommands (stubs wired with correct Use/Short/Args, returning "not yet implemented" — filled in by T5–T8).

**Implements:** FR-001, FR-002, FR-003 (stub)

**Deliverables:**
- `cmd/gcx/setup/command.go` — `setup.Command()` with status subcommand
- `cmd/gcx/setup/instrumentation/command.go` — instrumentation group command
- `cmd/gcx/root/command.go` — modified to add `setup.Command()`

**Acceptance criteria:**
- GIVEN gcx is built with the setup package imported WHEN `gcx setup --help` is executed THEN the output MUST list `instrumentation` as a subcommand and `status` as a subcommand
- GIVEN gcx is built with the setup package imported WHEN `gcx --help` is executed THEN the output MUST list `setup` as a top-level command
- The `setup` area MUST NOT register via `providers.Register()`. It MUST be wired directly via `rootCmd.AddCommand()`.

## Wave 2: Commands

### T5: `gcx setup instrumentation status` command
**Priority**: P1
**Effort**: Medium
**Depends on**: T3, T4
**Type**: task

Implement the `status` command in `cmd/gcx/setup/instrumentation/status.go`. The command composes status from two sources: (1) `RunK8sMonitoring` via the instrumentation client to get per-cluster instrumentation state, and (2) a Prometheus query (`sum by (k8s_cluster_name) (increase(beyla_instrumentation_errors_total[1h]))`) using the existing `internal/query/prometheus/` client against the stack's Mimir endpoint. Results are merged client-side into a per-cluster view. Supports `--cluster <name>` filter flag. Default output is table format; `--output json|yaml` via output codec system. Also wire the aggregated `gcx setup status` to call instrumentation's status provider.

**Implements:** FR-003, FR-004, FR-005, FR-006, FR-041, FR-042, FR-043, FR-044, FR-045, FR-046, FR-047

**Deliverables:**
- `cmd/gcx/setup/instrumentation/status.go` — status command with table codec, cluster filter, output options
- `cmd/gcx/setup/instrumentation/status_test.go` — unit tests (table output shape, cluster filter, JSON output)
- `cmd/gcx/setup/command.go` — aggregated status wired to call instrumentation status

**Acceptance criteria:**
- GIVEN a valid cloud config pointing to a stack with Fleet Management enabled WHEN `gcx setup instrumentation status` is executed THEN the command MUST display per-cluster instrumentation state and Beyla error counts
- GIVEN `--cluster prod-1` flag WHEN `gcx setup instrumentation status --cluster prod-1` is executed THEN the output MUST contain only status for `prod-1`
- GIVEN a valid cloud config WHEN `gcx setup instrumentation status --output json` is executed THEN the output MUST be valid JSON
- GIVEN instrumentation is the only registered setup product WHEN `gcx setup status` is executed THEN a table MUST include an `instrumentation` row
- GIVEN any error WHEN the error message is inspected THEN it MUST be prefixed with `"setup/instrumentation: "`

---

### T6: `gcx setup instrumentation discover` command
**Priority**: P1
**Effort**: Medium
**Depends on**: T3, T4
**Type**: task

Implement the `discover` command in `cmd/gcx/setup/instrumentation/discover.go`. The command calls `SetupK8sDiscovery` to initialize discovery, then `RunK8sDiscovery` to execute, returning discovered namespaces and workloads. `--cluster <name>` is required. Default output is table with columns: NAMESPACE, WORKLOAD, TYPE, INSTRUMENTATION STATE. `--output json|yaml` supported. If no workloads are discovered, print an informational message.

**Implements:** FR-007, FR-008, FR-009, FR-041, FR-042, FR-043, FR-044, FR-045, FR-046, FR-047

**Deliverables:**
- `cmd/gcx/setup/instrumentation/discover.go` — discover command with table codec, required --cluster flag
- `cmd/gcx/setup/instrumentation/discover_test.go` — unit tests (table output, missing cluster flag error, empty results message)

**Acceptance criteria:**
- GIVEN a valid cloud config and a cluster with workloads WHEN `gcx setup instrumentation discover --cluster prod-1` is executed THEN the output MUST list workloads in table format
- GIVEN no `--cluster` flag WHEN `gcx setup instrumentation discover` is executed THEN the command MUST exit non-zero with an error requiring `--cluster`
- GIVEN a cluster with no workloads WHEN `gcx setup instrumentation discover --cluster empty-cluster` is executed THEN the command MUST print an informational message
- GIVEN any error WHEN the error message is inspected THEN it MUST be prefixed with `"setup/instrumentation: "`

---

### T7: `gcx setup instrumentation show` command
**Priority**: P1
**Effort**: Medium
**Depends on**: T3, T4
**Type**: task

Implement the `show` command in `cmd/gcx/setup/instrumentation/show.go`. The command takes a required `<cluster>` positional argument, calls `GetAppInstrumentation` and `GetK8SInstrumentation`, and assembles a complete `InstrumentationConfig` manifest. Default output is YAML. If a cluster has no instrumentation configured, output a manifest with empty spec (not an error). Datasource URLs are NOT included in the manifest.

**Implements:** FR-010, FR-011, FR-012, FR-013, FR-014, FR-041, FR-042, FR-043, FR-045, FR-046, FR-047

**Deliverables:**
- `cmd/gcx/setup/instrumentation/show.go` — show command with YAML default, positional cluster arg
- `cmd/gcx/setup/instrumentation/show_test.go` — unit tests (YAML output, JSON output, missing arg error, empty cluster)

**Acceptance criteria:**
- GIVEN a cluster with app instrumentation for namespaces `frontend` and `data` WHEN `gcx setup instrumentation show prod-1` is executed THEN the output MUST be a valid YAML InstrumentationConfig manifest with the correct apiVersion, kind, and namespace entries
- GIVEN a cluster with K8s monitoring WHEN `gcx setup instrumentation show prod-1 -o json` is executed THEN the output MUST be valid JSON with `spec.k8s` populated
- GIVEN no `<cluster>` argument WHEN `gcx setup instrumentation show` is executed THEN the command MUST exit non-zero with an error
- GIVEN a cluster with no config WHEN `gcx setup instrumentation show unconfigured-cluster` is executed THEN the output MUST be a valid manifest with empty spec (not an error)
- GIVEN any error WHEN the error message is inspected THEN it MUST be prefixed with `"setup/instrumentation: "`

---

### T8: `gcx setup instrumentation apply` command
**Priority**: P1
**Effort**: Medium-Large
**Depends on**: T3, T4
**Type**: task

Implement the `apply` command in `cmd/gcx/setup/instrumentation/apply.go`. The command reads an `InstrumentationConfig` manifest from `-f <file>` (required), validates it (apiVersion, kind, metadata.name), performs optimistic locking (GET remote → compare via `compare.Compare` → fail if remote has extras), then calls `SetAppInstrumentation` and/or `SetK8SInstrumentation` as appropriate. Absent `spec.app` or `spec.k8s` sections MUST NOT trigger their respective SET calls. `--dry-run` performs GET + compare but skips SET calls, printing a summary. Auto-populates datasource URLs from stack context at apply time.

**Implements:** FR-015, FR-016, FR-017, FR-018, FR-019, FR-020, FR-021, FR-022, FR-023, FR-041, FR-042, FR-043, FR-046, FR-047

**Deliverables:**
- `cmd/gcx/setup/instrumentation/apply.go` — apply command with -f flag, dry-run, optimistic locking
- `cmd/gcx/setup/instrumentation/apply_test.go` — unit tests (happy path, optimistic lock failure, dry-run, validation errors, absent sections, datasource auto-population)

**Acceptance criteria:**
- GIVEN a valid manifest with `spec.app` defined WHEN `gcx setup instrumentation apply -f config.yaml` is executed THEN the command MUST GET remote config, verify no remote-only state, and call `SetAppInstrumentation`
- GIVEN a manifest with both `spec.app` and `spec.k8s` WHEN apply is executed THEN MUST call both `SetAppInstrumentation` and `SetK8SInstrumentation`
- GIVEN a manifest with only `spec.k8s` WHEN apply is executed THEN MUST call only `SetK8SInstrumentation` and MUST NOT call `SetAppInstrumentation`
- GIVEN remote has namespace `monitoring` not in local manifest WHEN apply is executed THEN MUST fail with error listing remote-only items and suggesting `show -o yaml`
- GIVEN a local superset of remote WHEN apply is executed THEN MUST succeed
- GIVEN `--dry-run` WHEN apply is executed THEN MUST print summary but MUST NOT execute SET calls
- GIVEN `apiVersion: wrong/v1` WHEN apply is executed THEN MUST fail with validation error before any API call
- GIVEN no `-f` flag WHEN apply is executed THEN MUST exit non-zero with error requiring `-f`
- GIVEN manifest from stack A WHEN `apply -f config.yaml --context stack-b` is executed THEN MUST apply to stack B with auto-populated datasource URLs
- GIVEN any error WHEN the error message is inspected THEN it MUST be prefixed with `"setup/instrumentation: "`

## Wave 3: Documentation and Verification

### T9: Documentation updates and verification gates
**Priority**: P1
**Effort**: Medium
**Depends on**: T1, T2, T3, T4, T5, T6, T7, T8
**Type**: chore

Update all documentation to reflect the new `setup` command area and `internal/fleet/` package. Run `GCX_AGENT_MODE=false make all` to verify lint, tests, build, and docs generation all pass. Run smoke tests against a live stack.

**Implements:** Verification gates from spec acceptance criteria

**Deliverables:**
- `docs/architecture/project-structure.md` — add `cmd/gcx/setup/`, `cmd/gcx/setup/instrumentation/`, `internal/fleet/`, `internal/setup/instrumentation/`
- `docs/architecture/architecture.md` — reference the `setup` command area
- `docs/architecture/cli-layer.md` — add `setup` to the command tree diagram
- `docs/architecture/patterns.md` — document any new patterns (optimistic lock comparison, composed status)
- `CONSTITUTION.md` — reference the `setup` area's command grammar
- `DESIGN.md` — update Pipeline section, Package Map, and ADR table
- `CLAUDE.md` — add `cmd/gcx/setup/` and `internal/fleet/` to Package Map

**Acceptance criteria:**
- GIVEN all implementation is complete WHEN `GCX_AGENT_MODE=false make all` is executed THEN lint, tests, build, and docs generation MUST all pass
- GIVEN new packages added WHEN `docs/architecture/` is inspected THEN `project-structure.md` MUST list new packages, `architecture.md` MUST reference setup area, `patterns.md` MUST document new patterns
- GIVEN the new `setup` area WHEN `CONSTITUTION.md` is inspected THEN it MUST reference the `setup` area's command grammar
- GIVEN the new command area and shared package WHEN `DESIGN.md` is inspected THEN Pipeline section MUST include setup, Package Map MUST list new packages, ADR table MUST reference ADR 013
- GIVEN all smoke tests defined in spec WHEN executed against a live stack THEN all MUST pass
