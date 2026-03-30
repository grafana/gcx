---
type: feature-spec
title: "Declarative Instrumentation Setup under gcx setup"
status: done
research: docs/research/2026-03-30-setup-framework.md
beads_id: gcx-4r1g
created: 2026-03-30
---

# Declarative Instrumentation Setup under gcx setup

## Problem Statement

Grafana Cloud's Instrumentation Hub provides a control plane for discovering Kubernetes services and applying observability instrumentation at scale. The underlying API is the Fleet Management gRPC/Connect service (`instrumentation.v1.InstrumentationService` and `discovery.v1.DiscoveryService`).

The existing `grafana-cloud-cli` exposes this imperatively via `gcx fleet instrumentation {app|k8s} {get|set}` with flag-based and file-based mutation, plus `fleet discovery {setup|run-app|run-k8s}`. Each command mutates server state directly — there is no local representation, no portable config, and no drift detection.

**gcx has no instrumentation support today.** The fleet provider handles only pipelines and collectors — low-level primitives that instrumentation operates on top of. Users who manage instrumentation programmatically or via CI/CD must use the old `grafana-cloud-cli` binary or call the REST APIs directly. AI agents that need to discover and instrument services autonomously have no gcx surface to work with.

The workaround is to use `grafana-cloud-cli` (which is being deprecated) or to call the Fleet Management API directly via `curl` / custom scripts.

## Scope

### In Scope

- New top-level `gcx setup` command area wired directly into the root command (like `dev` and `config`)
- `gcx setup status` aggregated status across all registered setup products
- `gcx setup instrumentation status [--cluster <name>]` — show Alloy collector presence and Beyla health
- `gcx setup instrumentation discover --cluster <name>` — find workloads and namespaces via discovery API
- `gcx setup instrumentation show <cluster> [-o yaml|json]` — current config as portable InstrumentationConfig manifest
- `gcx setup instrumentation apply -f <file> [--dry-run]` — apply InstrumentationConfig manifest with optimistic locking
- Declarative `InstrumentationConfig` manifest format (`setup.grafana.app/v1alpha1`) as a plain Go struct with YAML/JSON tags
- Shared fleet client extraction: `internal/fleet/` package with base HTTP client, auth, error types
- Fleet-managed pipeline protection guard on fleet provider's `pipeline update` and `pipeline delete` for `beyla_k8s_appo11y_` prefixed pipelines
- Instrumentation HTTP client for `instrumentation.v1.InstrumentationService` and `discovery.v1.DiscoveryService` Connect endpoints
- Auth via `LoadCloudConfig()` → StackInfo → Basic auth `{instanceID}:{apiToken}` (same as fleet provider)
- JSON and YAML output for all commands via the output codec system
- Table output for `status` and `discover` commands
- Unit tests for all new packages with table-driven test pattern
- `--context` flag support for cross-stack portability

### Out of Scope

- **Stage 2 imperative `add` verb** — flag-based fetch-modify-apply for quick edits. Deferred to validate declarative approach first.
- **`SetupProduct` formal interface and `setup.Register()` framework** — Stage 1 uses direct wiring. The formal product registration interface is deferred to the research phase documented in `docs/research/2026-03-30-setup-framework.md`.
- **Directory-based pull/apply across multiple setup products** — only single-file apply for instrumentation in Stage 1.
- **Other setup products** (KG setup, integrations install, auth bootstrap) — `gcx setup` is designed for extensibility but only instrumentation ships in Stage 1.
- **Integration/e2e tests against live Grafana Cloud** — unit tests with HTTP mocks only.
- **TypedCRUD adapter registration for InstrumentationConfig** — the manifest is not a CRUD resource; it does not register via `adapter.Registration` or participate in `gcx resources push/pull`.
- **K8s dynamic client integration** — InstrumentationConfig is a plain Go struct, not a K8s resource processed through the dynamic client pipeline.
- **connectrpc library dependency** — hand-rolled JSON-over-HTTP Connect protocol (POST to `/service.v1.ServiceName/MethodName`), consistent with existing fleet client pattern.

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Command area | New top-level `gcx setup` with `instrumentation` as first product | Instrumentation is onboarding (discover → configure → verify), not CRUD. The pattern recurs across products. `gcx setup` is extensible. | ADR Section "Instrumentation lives under gcx setup" |
| Wiring approach | Direct `rootCmd.AddCommand(setup.Command())` — NOT a provider | `setup` is an area like `dev` or `config`, not a provider. No `providers.Register()`, no `TypedRegistrations()`. | ADR Section "Rejected alternatives" |
| Manifest format | `setup.grafana.app/v1alpha1` / `InstrumentationConfig` — plain Go struct with YAML/JSON tags | Environment-agnostic, portable across stacks. Cluster name as identity. Datasource URLs auto-populated from stack context. | ADR Section "Declarative manifest format" |
| Apply semantics | Optimistic locking: GET → compare → fail-if-remote-has-extra → SET | Prevents accidentally dropping configuration instrumented via UI or other tooling | ADR Section "Apply semantics" |
| Shared fleet client | Extract `internal/fleet/` package from existing fleet provider | Eliminates client code duplication between fleet provider and instrumentation commands. Both import shared base. | ADR Section "Shared fleet client package" |
| Pipeline protection | Guard `beyla_k8s_appo11y_` prefixed pipelines in fleet provider update/delete with `--force` override | Prevents low-level interference with fleet-managed Beyla pipelines created by instrumentation | ADR Section "Fleet-managed pipeline protection" |
| Auth mechanism | `LoadCloudConfig()` → StackInfo → Basic auth `{instanceID}:{apiToken}` | Same auth as fleet provider. No provider-specific config keys needed. | ADR Section "Same API server & auth as fleet" |
| Connect protocol | Hand-rolled JSON-over-HTTP POST to `/service.v1.ServiceName/MethodName` | No connectrpc library in the dependency tree. Matches existing fleet client pattern. | Codebase context |
| Delivery scope | Stage 1: declarative apply only. Stage 2: imperative `add` verb. | Validate declarative approach first. Imperative convenience is sugar over the declarative core. | ADR Section "Two-stage delivery" |

## Functional Requirements

**Setup Command Area**

- **FR-001**: The CLI MUST expose a top-level `gcx setup` command wired via `rootCmd.AddCommand(setup.Command())` in `cmd/gcx/root/command.go`.
- **FR-002**: `gcx setup` MUST have short description `"Onboard and configure Grafana Cloud products."`.
- **FR-003**: `gcx setup status` (without product subcommand) MUST aggregate status across all registered setup products and display a table with columns: PRODUCT, ENABLED, HEALTH, DETAILS.

**Instrumentation Status**

- **FR-004**: `gcx setup instrumentation status` MUST compose status from two sources: (1) `discovery.v1.DiscoveryService/RunK8sMonitoring` Connect endpoint to list clusters and their instrumentation state, and (2) a Prometheus query for `sum by (k8s_cluster_name) (increase(beyla_instrumentation_errors_total[1h]))` against the stack's Mimir to detect Beyla errors. Results are combined client-side into a per-cluster status view.
- **FR-005**: `gcx setup instrumentation status --cluster <name>` MUST filter status output to the specified cluster.
- **FR-006**: `gcx setup instrumentation status` MUST display results in table format by default, with `--output json|yaml` support via the output codec system.

**Instrumentation Discover**

- **FR-007**: `gcx setup instrumentation discover --cluster <name>` MUST use the discovery API (via `discovery.v1.DiscoveryService/SetupK8sDiscovery` to initialize discovery and `discovery.v1.DiscoveryService/RunK8sDiscovery` to execute) to find instrumentable workloads and namespaces in the specified cluster.
- **FR-008**: The `--cluster` flag on `discover` MUST be required. The command MUST fail with an actionable error if omitted.
- **FR-009**: `discover` MUST display results in table format by default with columns for namespace, workload name, workload type, and current instrumentation state. `--output json|yaml` MUST be supported.

**Instrumentation Show**

- **FR-010**: `gcx setup instrumentation show <cluster>` MUST call `instrumentation.v1.InstrumentationService/GetAppInstrumentation` and `instrumentation.v1.InstrumentationService/GetK8SInstrumentation` Connect endpoints to retrieve the current configuration for the specified cluster.
- **FR-011**: `show` MUST produce a complete `InstrumentationConfig` manifest (apiVersion, kind, metadata, spec) as the output.
- **FR-012**: `show` MUST default to YAML output format. `--output json|yaml` MUST be supported.
- **FR-013**: The `<cluster>` positional argument MUST be required. The command MUST fail with an actionable error if omitted.
- **FR-014**: If a cluster has no instrumentation configured, `show` MUST return a manifest with empty `spec.app` and `spec.k8s` sections (not an error).

**Instrumentation Apply**

- **FR-015**: `gcx setup instrumentation apply -f <file>` MUST read an `InstrumentationConfig` manifest from a YAML or JSON file.
- **FR-016**: `apply` MUST validate the manifest against the `InstrumentationConfig` struct. Invalid manifests (wrong apiVersion, wrong kind, missing metadata.name) MUST be rejected with a structured error before any API call.
- **FR-017**: `apply` MUST implement optimistic locking: (1) GET current remote config for the cluster named in `metadata.name`, (2) compare remote state with local manifest, (3) if remote has namespaces or workloads NOT present in the local manifest, fail with a clear error listing the extra items and suggesting `show -o yaml` to reconcile, (4) if manifest is a superset of or matches remote, proceed with SET.
- **FR-018**: When `spec.app` is present in the manifest, `apply` MUST call `instrumentation.v1.InstrumentationService/SetAppInstrumentation` with the app configuration.
- **FR-019**: When `spec.k8s` is present in the manifest, `apply` MUST call `instrumentation.v1.InstrumentationService/SetK8SInstrumentation` with the K8s monitoring configuration.
- **FR-020**: When `spec.app` is absent from the manifest, `apply` MUST NOT call `SetAppInstrumentation`. When `spec.k8s` is absent, `apply` MUST NOT call `SetK8SInstrumentation`. Absent sections are untouched.
- **FR-021**: `apply --dry-run` MUST perform the GET and comparison steps but MUST NOT execute any SET calls. It MUST print a summary of what would be applied.
- **FR-022**: The `-f` flag MUST be required. The command MUST fail with an actionable error if omitted.
- **FR-023**: `apply` MUST auto-populate datasource URLs (Mimir, Loki, Tempo, Pyroscope endpoints) from the target stack context. These URLs MUST NOT appear in the manifest.

**InstrumentationConfig Manifest**

- **FR-024**: The `InstrumentationConfig` struct MUST use apiVersion `setup.grafana.app/v1alpha1` and kind `InstrumentationConfig`.
- **FR-025**: `metadata.name` MUST represent the cluster name (resource identity).
- **FR-026**: `spec.app` MUST support: `namespaces` array where each entry has `name` (string), `selection` (included|excluded), `tracing` (bool), `processmetrics` (bool), `extendedmetrics` (bool), `logging` (bool), `profiling` (bool), and optional `apps` array for per-workload overrides.
- **FR-027**: Each `apps` entry MUST have `name` (string), `selection` (included|excluded), and optional `type` (string, workload type). Per-workload entries do NOT support signal toggles — signals are configured at namespace level only.
- **FR-028**: `spec.k8s` MUST support: `costmetrics` (bool), `energymetrics` (bool), `clusterevents` (bool), `nodelogs` (bool).
- **FR-029**: The struct MUST have both `yaml` and `json` struct tags on all fields.

**Shared Fleet Client (`internal/fleet/`)**

- **FR-030**: A new `internal/fleet/` package MUST be created containing the base HTTP client, auth helper, and error types extracted from `internal/providers/fleet/client.go`.
- **FR-031**: The base client MUST implement `doRequest(ctx, path, body) (*http.Response, error)` with JSON-over-HTTP Connect protocol (POST method, `Content-Type: application/json`, `Accept: application/json`).
- **FR-032**: The base client MUST support Basic auth (`{instanceID}:{apiToken}`) and Bearer token auth, selected at construction time.
- **FR-033**: The base client MUST use `providers.ExternalHTTPClient()` as the default transport when no custom `*http.Client` is provided.
- **FR-034**: `internal/fleet/config.go` MUST provide a `LoadClient(ctx) (*Client, error)` function (or equivalent) that loads credentials via `LoadCloudConfig()`, extracts `AgentManagementInstanceURL` and `AgentManagementInstanceID` from StackInfo, and returns a configured client.
- **FR-035**: The existing fleet provider (`internal/providers/fleet/`) MUST be refactored to import and use `internal/fleet/` for its base HTTP client. All existing fleet provider tests MUST continue to pass.

**Instrumentation Client**

- **FR-036**: An instrumentation-specific client MUST be built on top of the `internal/fleet/` base client, adding methods for the `instrumentation.v1.InstrumentationService` and `discovery.v1.DiscoveryService` Connect endpoints.
- **FR-037**: The instrumentation client MUST implement methods for: `GetAppInstrumentation`, `SetAppInstrumentation`, `GetK8SInstrumentation`, `SetK8SInstrumentation` (instrumentation service), and `SetupK8sDiscovery`, `RunK8sDiscovery`, `RunK8sMonitoring` (discovery service).

**Fleet-Managed Pipeline Protection**

- **FR-038**: The fleet provider's `pipeline update` command MUST reject updates to pipelines whose name starts with `beyla_k8s_appo11y_` with an error message directing users to `gcx setup instrumentation apply` instead.
- **FR-039**: The fleet provider's `pipeline delete` command MUST reject deletion of pipelines whose name starts with `beyla_k8s_appo11y_` with an error message directing users to `gcx setup instrumentation apply` instead.
- **FR-040**: Both `pipeline update` and `pipeline delete` MUST accept a `--force` flag that overrides the protection guard.

**Auth**

- **FR-041**: All instrumentation commands MUST authenticate using `LoadCloudConfig()` → StackInfo → Basic auth with `{instanceID}:{apiToken}` where `instanceID` is `AgentManagementInstanceID` and `apiToken` is the cloud config token.
- **FR-042**: If `AgentManagementInstanceURL` or `AgentManagementInstanceID` is missing from StackInfo, commands MUST fail with a structured error explaining that Fleet Management is not available for the stack and suggesting how to configure it.

**Output**

- **FR-043**: All instrumentation commands MUST support `--output` flag with at minimum `json` and `yaml` formats via the output codec system.
- **FR-044**: `status` and `discover` commands MUST additionally support `text` and `wide` table formats.
- **FR-045**: `show` MUST default to `yaml` output format. All other commands MUST default to `text` (table) format.

**Error Handling**

- **FR-046**: All errors from instrumentation commands MUST be prefixed with `"setup/instrumentation: "`.
- **FR-047**: HTTP errors from the Fleet Management API MUST be translated into user-facing messages that include the HTTP status code and response body.

## Acceptance Criteria

**Setup area wiring**

- GIVEN gcx is built with the setup package imported
  WHEN `gcx setup --help` is executed
  THEN the output MUST list `instrumentation` as a subcommand and `status` as a subcommand

- GIVEN gcx is built with the setup package imported
  WHEN `gcx --help` is executed
  THEN the output MUST list `setup` as a top-level command

**Aggregated status**

- GIVEN instrumentation is the only registered setup product
  WHEN `gcx setup status` is executed with valid cloud config
  THEN a table MUST be printed with one row for `instrumentation` showing ENABLED, HEALTH, and DETAILS columns

**Instrumentation status**

- GIVEN a valid cloud config pointing to a stack with Fleet Management enabled
  WHEN `gcx setup instrumentation status` is executed
  THEN the command MUST display per-cluster instrumentation state (from RunK8sMonitoring) and Beyla error counts (from Prometheus query)

- GIVEN a valid cloud config and `--cluster prod-1` flag
  WHEN `gcx setup instrumentation status --cluster prod-1` is executed
  THEN the output MUST contain only status for the `prod-1` cluster

- GIVEN a valid cloud config
  WHEN `gcx setup instrumentation status --output json` is executed
  THEN the output MUST be valid JSON containing cluster status information

**Instrumentation discover**

- GIVEN a valid cloud config and a cluster with running workloads
  WHEN `gcx setup instrumentation discover --cluster prod-1` is executed
  THEN the output MUST list discovered namespaces and workloads in table format with columns for namespace, workload name, workload type, and instrumentation state

- GIVEN no `--cluster` flag is provided
  WHEN `gcx setup instrumentation discover` is executed
  THEN the command MUST exit with a non-zero exit code and print an error requiring the `--cluster` flag

- GIVEN a cluster with no discovered workloads
  WHEN `gcx setup instrumentation discover --cluster empty-cluster` is executed
  THEN the command MUST print an informational message indicating no workloads were discovered

**Instrumentation show**

- GIVEN a cluster `prod-1` with app instrumentation configured for namespaces `frontend` and `data`
  WHEN `gcx setup instrumentation show prod-1` is executed
  THEN the output MUST be a valid YAML `InstrumentationConfig` manifest with `apiVersion: setup.grafana.app/v1alpha1`, `kind: InstrumentationConfig`, `metadata.name: prod-1`, and `spec.app.namespaces` containing entries for `frontend` and `data`

- GIVEN a cluster `prod-1` with K8s monitoring enabled
  WHEN `gcx setup instrumentation show prod-1 -o json` is executed
  THEN the output MUST be valid JSON with `spec.k8s` fields populated

- GIVEN no `<cluster>` positional argument
  WHEN `gcx setup instrumentation show` is executed
  THEN the command MUST exit with a non-zero exit code and print an error requiring the cluster argument

- GIVEN a cluster with no instrumentation configured
  WHEN `gcx setup instrumentation show unconfigured-cluster` is executed
  THEN the output MUST be a valid manifest with empty `spec` (not an error)

**Instrumentation apply — happy path**

- GIVEN a valid `InstrumentationConfig` manifest file `config.yaml` with `metadata.name: prod-1` and `spec.app` defined
  WHEN `gcx setup instrumentation apply -f config.yaml` is executed
  THEN the command MUST GET current remote config for `prod-1`, verify no remote-only state exists, and call `SetAppInstrumentation` with the app configuration

- GIVEN a manifest with both `spec.app` and `spec.k8s` defined
  WHEN `gcx setup instrumentation apply -f config.yaml` is executed
  THEN the command MUST call both `SetAppInstrumentation` and `SetK8SInstrumentation`

- GIVEN a manifest with only `spec.k8s` defined (no `spec.app`)
  WHEN `gcx setup instrumentation apply -f config.yaml` is executed
  THEN the command MUST call only `SetK8SInstrumentation` and MUST NOT call `SetAppInstrumentation`

**Instrumentation apply — optimistic locking**

- GIVEN a remote config that has namespace `monitoring` instrumented but the local manifest does not include `monitoring`
  WHEN `gcx setup instrumentation apply -f config.yaml` is executed
  THEN the command MUST fail with a non-zero exit code and print an error listing `monitoring` as a remote-only namespace, suggesting `gcx setup instrumentation show prod-1 -o yaml` to reconcile

- GIVEN a local manifest that is a superset of remote config (adds new namespace not present remotely)
  WHEN `gcx setup instrumentation apply -f config.yaml` is executed
  THEN the command MUST succeed and apply the configuration

**Instrumentation apply — dry-run**

- GIVEN a valid manifest file
  WHEN `gcx setup instrumentation apply -f config.yaml --dry-run` is executed
  THEN the command MUST print what would be applied (sections, namespaces, signals) but MUST NOT execute any SET API calls

**Instrumentation apply — validation**

- GIVEN a manifest file with `apiVersion: wrong/v1`
  WHEN `gcx setup instrumentation apply -f wrong.yaml` is executed
  THEN the command MUST fail with a validation error before making any API call

- GIVEN a manifest file with missing `metadata.name`
  WHEN `gcx setup instrumentation apply -f noname.yaml` is executed
  THEN the command MUST fail with a validation error stating that `metadata.name` (cluster name) is required

- GIVEN no `-f` flag
  WHEN `gcx setup instrumentation apply` is executed
  THEN the command MUST exit with a non-zero exit code and print an error requiring the `-f` flag

**Manifest portability**

- GIVEN a manifest produced by `show` on stack A
  WHEN `gcx setup instrumentation apply -f config.yaml --context stack-b` is executed
  THEN the manifest MUST be applied to stack B with datasource URLs auto-populated from stack B's context

- GIVEN an `InstrumentationConfig` manifest
  WHEN the manifest is inspected
  THEN it MUST NOT contain any datasource URLs, instance IDs, API tokens, or stack-specific values

**Shared fleet client extraction**

- GIVEN the `internal/fleet/` package exists
  WHEN `internal/providers/fleet/client.go` is inspected
  THEN it MUST import and delegate to `internal/fleet/` for base HTTP operations (doRequest, auth)

- GIVEN the `internal/fleet/` refactoring is complete
  WHEN `make tests` is executed
  THEN all existing fleet provider tests MUST pass without modification to test assertions

**Fleet-managed pipeline protection**

- GIVEN a pipeline named `beyla_k8s_appo11y_prod-1`
  WHEN `gcx fleet pipelines update <id>` is executed without `--force`
  THEN the command MUST fail with an error stating the pipeline is managed by instrumentation and directing the user to `gcx setup instrumentation apply`

- GIVEN a pipeline named `beyla_k8s_appo11y_prod-1`
  WHEN `gcx fleet pipelines delete <id>` is executed without `--force`
  THEN the command MUST fail with an error stating the pipeline is managed by instrumentation and directing the user to `gcx setup instrumentation apply`

- GIVEN a pipeline named `beyla_k8s_appo11y_prod-1`
  WHEN `gcx fleet pipelines update <id> --force` is executed
  THEN the update MUST proceed normally

- GIVEN a pipeline named `beyla_k8s_appo11y_prod-1`
  WHEN `gcx fleet pipelines delete <id> --force` is executed
  THEN the delete MUST proceed normally

- GIVEN a pipeline named `my-custom-pipeline` (no `beyla_k8s_appo11y_` prefix)
  WHEN `gcx fleet pipelines update <id>` is executed without `--force`
  THEN the update MUST proceed normally (no guard triggered)

**Auth**

- GIVEN a valid cloud config with stack name and AP token
  WHEN any instrumentation command is executed
  THEN the HTTP request MUST contain an `Authorization: Basic {base64({instanceID}:{apiToken})}` header

- GIVEN a cloud config without a stack name or AP token
  WHEN any instrumentation command is executed
  THEN the command MUST return a structured error with an actionable suggestion to configure cloud credentials

- GIVEN a stack where `AgentManagementInstanceURL` is empty
  WHEN any instrumentation command is executed
  THEN the command MUST fail with an error stating Fleet Management is not available for the stack

**Error prefixes**

- GIVEN any error from an instrumentation command
  WHEN the error message is inspected
  THEN it MUST be prefixed with `"setup/instrumentation: "`

**Verification gates**

- GIVEN all implementation is complete
  WHEN `GCX_AGENT_MODE=false make all` is executed
  THEN lint, tests, build, and docs generation MUST all pass with zero failures

- GIVEN new packages have been added under `cmd/gcx/setup/` and `internal/fleet/`
  WHEN `docs/architecture/` is inspected
  THEN `project-structure.md` MUST list the new packages, `architecture.md` MUST reference the `setup` command area, and `patterns.md` MUST document any new patterns introduced

- GIVEN the implementation adds a new top-level `setup` area
  WHEN `CONSTITUTION.md` is inspected
  THEN it MUST be updated to reference the `setup` area's command grammar (since `setup` is an area like `dev` and `config`, not a provider)

- GIVEN the implementation adds a new command area and shared package
  WHEN `DESIGN.md` is inspected
  THEN the Pipeline section MUST include the setup extension pipeline, the Package Map MUST list `cmd/gcx/setup/` and `internal/fleet/`, and the ADR table MUST reference ADR 013

- GIVEN the implementation is complete
  WHEN all smoke tests (see Smoke Tests section) are executed against a live stack
  THEN all smoke tests MUST pass

**Smoke tests**

- GIVEN a configured gcx context pointing to a Grafana Cloud stack with Fleet Management enabled and at least one cluster with Alloy running
  WHEN `gcx setup instrumentation status` is executed
  THEN the command MUST exit 0 and print a table with at least one cluster row

- GIVEN a configured gcx context and a known cluster name
  WHEN `gcx setup instrumentation discover --cluster <name>` is executed
  THEN the command MUST exit 0 and print discovered workloads (or an empty-result message if no workloads exist)

- GIVEN a configured gcx context and a known cluster name
  WHEN `gcx setup instrumentation show <cluster>` is executed
  THEN the command MUST exit 0 and produce valid YAML with `apiVersion: setup.grafana.app/v1alpha1`

- GIVEN a configured gcx context and a manifest file produced by `show`
  WHEN `gcx setup instrumentation apply -f <file> --dry-run` is executed
  THEN the command MUST exit 0 and print a dry-run summary (no mutations)

- GIVEN a configured gcx context and an unmodified manifest from `show`
  WHEN `gcx setup instrumentation apply -f <file>` is executed
  THEN the command MUST exit 0 (no-op apply since manifest matches remote)

- GIVEN a configured gcx context
  WHEN `gcx setup instrumentation show <cluster> -o json` is executed
  THEN the output MUST be valid JSON parseable by `jq .`

- GIVEN a configured gcx context
  WHEN `gcx setup status` is executed
  THEN the command MUST exit 0 and include an `instrumentation` row in the output table

- GIVEN the fleet provider has the pipeline protection guard
  WHEN `gcx fleet pipelines list` is executed and any pipeline name starts with `beyla_k8s_appo11y_`
  THEN `gcx fleet pipelines delete <id>` (without `--force`) MUST exit non-zero with a guard error

## Negative Constraints

- **NC-001**: The `setup` area MUST NOT register via `providers.Register()`. It MUST be wired directly into the root command via `rootCmd.AddCommand()`.
- **NC-002**: `InstrumentationConfig` MUST NOT be processed through the K8s dynamic client pipeline. It is a plain Go struct with YAML/JSON tags.
- **NC-003**: `InstrumentationConfig` manifests MUST NOT contain datasource URLs, instance IDs, API tokens, or any stack-specific values. These MUST be auto-populated from the target stack context at apply time.
- **NC-004**: `apply` MUST NOT blindly replace remote state. It MUST implement the optimistic locking comparison and fail if remote has state not present in the manifest.
- **NC-005**: `apply` MUST NOT call `SetAppInstrumentation` when `spec.app` is absent from the manifest. It MUST NOT call `SetK8SInstrumentation` when `spec.k8s` is absent.
- **NC-006**: The instrumentation client MUST NOT add a `connectrpc` library dependency. It MUST use hand-rolled JSON-over-HTTP Connect protocol consistent with the existing fleet client.
- **NC-007**: The fleet provider's `pipeline update` and `pipeline delete` MUST NOT allow modification of `beyla_k8s_appo11y_` prefixed pipelines without `--force`.
- **NC-008**: The `show` command MUST NOT return an error when a cluster has no instrumentation configured. It MUST return a valid manifest with empty spec.
- **NC-009**: Instrumentation commands MUST NOT require any user-configured provider-specific config keys. Auth MUST be derived entirely from `LoadCloudConfig()`.
- **NC-010**: The `internal/fleet/` extraction MUST NOT change any observable behavior of the existing fleet provider commands. All existing tests MUST pass.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Instrumentation API endpoint paths are not publicly documented and may change | Client code becomes incompatible, commands fail | Pin to `instrumentation.v1` and `discovery.v1` service names. Version is in the Connect path. Add integration smoke test in CI when endpoints stabilize. |
| Optimistic locking comparison logic may have edge cases (empty namespaces, default values) | False positives blocking valid applies, or false negatives allowing accidental drops | Comprehensive unit tests covering: empty remote, empty local, superset, subset, exact match, per-workload override differences. Test with real API response shapes. |
| `internal/fleet/` extraction may introduce import cycles or break downstream packages | Build failure, blocked development | Extract in a standalone PR before instrumentation work. Run `make all` after extraction. Keep the public API surface minimal. |
| Fleet-managed pipeline protection guard may block legitimate fleet operations | Users unable to manage pipelines they own | `--force` override flag with clear warning message. Guard checks only the name prefix, not ownership metadata. |
| Connect endpoint response format differs from fleet provider's pipeline/collector format | Deserialization failures, incorrect data | Unit tests with captured real API responses. Separate request/response types for instrumentation endpoints — do not reuse fleet types. |
| Discovery API may return large result sets for clusters with many workloads | Slow or OOM on `discover` command | Paginate if API supports it. Set reasonable default timeout. Display partial results with a truncation indicator if result set exceeds threshold. |
| `show` for a cluster with partial config (only app, no k8s) may produce confusing output | Users unsure whether empty sections mean "not configured" or "explicitly disabled" | Omit absent sections from output (do not emit `spec.k8s: {}`) with a comment or note in `--output text` mode. |

## Open Questions

- [RESOLVED] Command area: `gcx setup` chosen over `gcx instrumentation` or extending `gcx fleet`. Rationale: onboarding pattern recurs across products; `setup` is the right abstraction level.
- [RESOLVED] Provider vs direct wiring: Direct `rootCmd.AddCommand()` chosen. `setup` is an area like `dev`, not a provider.
- [RESOLVED] Manifest format: `setup.grafana.app/v1alpha1` / `InstrumentationConfig` with cluster name as identity. Environment-agnostic.
- [RESOLVED] Apply semantics: Optimistic locking (GET → compare → fail-if-remote-has-extra → SET).
- [RESOLVED] Shared client: Extract `internal/fleet/` from fleet provider. Both fleet provider and instrumentation import from it.
- [RESOLVED] Pipeline protection: Guard `beyla_k8s_appo11y_` prefix in fleet provider update/delete with `--force` override.
- [RESOLVED] Stage 1 vs Stage 2: Stage 1 is declarative apply only. Imperative `add` deferred.
- [RESOLVED] Connect endpoint paths confirmed from `grafana-cloud-cli` source: Instrumentation service uses `GetAppInstrumentation`, `SetAppInstrumentation`, `GetK8SInstrumentation`, `SetK8SInstrumentation`. Discovery service uses `SetupK8sDiscovery`, `RunK8sDiscovery`, `RunK8sMonitoring`. No `GetInstrumentationStatus` endpoint exists.
- [RESOLVED] Status is composed client-side from two sources: `RunK8sMonitoring` (cluster list + instrumentation state) and a Prometheus query for `beyla_instrumentation_errors_total` (Beyla error counts). No single status endpoint.
- [RESOLVED] Per-workload `apps` entries have only `name`, `selection`, and `type` — no signal toggles. Signal toggles (tracing, logging, profiling, processmetrics, extendedmetrics, autoinstrument) are namespace-level only.
- [DEFERRED] `SetupProduct` formal interface and `setup.Register()` — deferred to Phase 2 framework design.
- [DEFERRED] Resource adapter integration for `InstrumentationConfig` via `TypedRegistrations()` — evaluate after Stage 1 ships.
- [DEFERRED] Directory-based multi-product pull/apply — deferred to Phase 2.
