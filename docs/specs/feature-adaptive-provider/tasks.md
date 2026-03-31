---
type: feature-tasks
title: "Adaptive Telemetry Provider"
status: done
spec: docs/specs/feature-adaptive-provider/spec.md
plan: docs/specs/feature-adaptive-provider/plan.md
created: 2026-03-27
---

# Implementation Tasks

## Dependency Graph

```
T1 (provider shell + auth) ──┬──→ T2 (metrics client + commands)
                              ├──→ T3 (logs client + commands + adapter)
                              └──→ T4 (traces client + commands + adapter)
                                        │
                              T2,T3,T4 ──→ T5 (integration wiring + make all)
```

## Wave 1: Provider Shell and Shared Auth

### T1: Provider registration, shared auth helper, and GCOM caching

**Priority**: P0
**Effort**: Medium
**Depends on**: none
**Type**: task

Implement the `adaptive` provider shell: the `Provider` interface implementation with `init()` self-registration, `Name()`, `ShortDesc()`, `ConfigKeys()`, `Validate()`, and empty `Commands()`/`TypedRegistrations()` stubs. Implement `auth.go` with shared Basic auth helper functions that resolve signal-specific instance URLs and IDs from `cloud.StackInfo` (via `LoadCloudConfig()`), cache tenant IDs/URLs via `SaveProviderConfig()`, and return the tuple needed by each signal client. The auth helper MUST check cached config first (`LoadProviderConfig()`) before falling back to GCOM lookup.

**Deliverables:**
- `internal/providers/adaptive/provider.go`
- `internal/providers/adaptive/provider_test.go`
- `internal/providers/adaptive/auth.go`
- `internal/providers/adaptive/auth_test.go`

**Acceptance criteria:**
- GIVEN a clean config
  WHEN `providers.All()` is called after import of the adaptive package
  THEN the adaptive provider appears in the returned slice with Name() == "adaptive"
- GIVEN a valid cloud config with GCOM credentials
  WHEN the auth helper is called for the "metrics" signal
  THEN it returns the HMInstancePromURL, HMInstancePromID, apiToken, and a non-nil *http.Client
- GIVEN a valid cloud config with GCOM credentials
  WHEN the auth helper is called for the "logs" signal
  THEN it returns the HLInstanceURL, HLInstanceID, apiToken, and a non-nil *http.Client
- GIVEN a valid cloud config with GCOM credentials
  WHEN the auth helper is called for the "traces" signal
  THEN it returns the HTInstanceURL, HTInstanceID, apiToken, and a non-nil *http.Client
- GIVEN a first successful GCOM lookup for a signal
  WHEN the auth helper completes
  THEN it writes `cloud.{signal}-tenant-id` and `cloud.{signal}-tenant-url` to provider config via `SaveProviderConfig()`
- GIVEN cached values exist in provider config for a signal
  WHEN the auth helper is called again
  THEN it returns the cached values without calling GCOM
- GIVEN `ConfigKeys()` is called on the adaptive provider
  THEN it returns keys for all six cached config entries (3 signals x 2 keys) with `Secret: false`

---

## Wave 2: Signal Clients and Commands (parallel)

### T2: Metrics HTTP client and commands (rules show/sync, recommendations show/apply)

**Priority**: P1
**Effort**: Medium
**Depends on**: T1
**Type**: task

Implement the metrics subpackage: domain types (`MetricRule`, `MetricRecommendation`), HTTP client for `/aggregations/rules` and `/aggregations/recommendations` endpoints, and four Cobra commands: `rules show`, `rules sync`, `recommendations show`, `recommendations apply`. All commands follow the options pattern with `cmdio.Options`, table/wide codecs, and `--dry-run` on destructive operations (`sync`, `apply`). The HTTP client uses `ExternalHTTPClient()` with Basic auth from the shared auth helper. Rules sync MUST implement ETag-based optimistic concurrency.

**Deliverables:**
- `internal/providers/adaptive/metrics/types.go`
- `internal/providers/adaptive/metrics/client.go`
- `internal/providers/adaptive/metrics/client_test.go`
- `internal/providers/adaptive/metrics/commands.go`

**Acceptance criteria:**
- GIVEN a metrics HTTP client configured with valid credentials
  WHEN `ListRules(ctx)` is called against a server returning JSON rules
  THEN it returns a slice of `MetricRule` with all fields populated
- GIVEN a metrics HTTP client configured with valid credentials
  WHEN `ListRecommendations(ctx)` is called
  THEN it returns a slice of `MetricRecommendation` with all fields populated
- GIVEN `gcx adaptive metrics rules show` is invoked with `--output table`
  WHEN rules exist
  THEN a formatted table is printed to stdout with one row per rule
- GIVEN `gcx adaptive metrics rules sync -f rules.json` is invoked
  WHEN rules are valid
  THEN the client GETs the current ETag and POSTs rules with `If-Match` header
- GIVEN `gcx adaptive metrics rules sync --dry-run` is invoked
  WHEN a rules file is provided
  THEN the command prints what would be synced without making any POST requests
- GIVEN `gcx adaptive metrics recommendations show` is invoked
  WHEN recommendations exist
  THEN a formatted table is printed to stdout
- GIVEN `gcx adaptive metrics recommendations apply --dry-run` is invoked
  THEN the command prints what would be applied without making any POST requests

---

### T3: Logs HTTP client, commands (patterns show/apply), and Exemption TypedCRUD adapter

**Priority**: P1
**Effort**: Medium-Large
**Depends on**: T1
**Type**: task

Implement the logs subpackage: domain types (`Exemption` with `ResourceIdentity`, `LogRecommendation`), HTTP client for `/adaptive-logs/exemptions` (full CRUD) and `/adaptive-logs/recommendations` endpoints, provider commands (`patterns show`, `patterns apply`), and the `TypedCRUD[Exemption]` adapter registration with `SchemaFromType[Exemption]()` and example JSON. The Exemption type MUST implement `GetResourceName()` and `SetResourceName()`. The exemptions API wraps list responses in `{"result": [...]}` — the client MUST handle this envelope. Patterns `apply` MUST support `--dry-run`.

**Deliverables:**
- `internal/providers/adaptive/logs/types.go`
- `internal/providers/adaptive/logs/client.go`
- `internal/providers/adaptive/logs/client_test.go`
- `internal/providers/adaptive/logs/resource_adapter.go`
- `internal/providers/adaptive/logs/commands.go`

**Acceptance criteria:**
- GIVEN a logs HTTP client configured with valid credentials
  WHEN `ListExemptions(ctx)` is called
  THEN it correctly unwraps the `{"result": [...]}` envelope and returns a slice of `Exemption`
- GIVEN a logs HTTP client configured with valid credentials
  WHEN `CreateExemption(ctx, exemption)` is called
  THEN it sends a POST to the exemptions endpoint and returns the created exemption
- GIVEN a logs HTTP client configured with valid credentials
  WHEN `DeleteExemption(ctx, id)` is called
  THEN it sends a DELETE to the exemptions endpoint
- GIVEN an `Exemption` value
  WHEN `GetResourceName()` is called
  THEN it returns the exemption's ID
- GIVEN a `*Exemption` value
  WHEN `SetResourceName("new-id")` is called
  THEN the exemption's ID field is updated
- GIVEN the adaptive provider is registered
  WHEN `TypedRegistrations()` includes the Exemption registration
  THEN it has a non-nil Schema and non-nil Example
- GIVEN `gcx adaptive logs patterns show` is invoked
  WHEN patterns exist
  THEN a formatted table is printed to stdout with pattern, drop rate, and volume columns
- GIVEN `gcx adaptive logs patterns apply "GET" --dry-run` is invoked
  THEN the command prints which patterns would be modified without POSTing
- GIVEN the Exemption adapter is registered
  WHEN `gcx resources get exemptions.v1alpha1.adaptive-logs.ext.grafana.app` is invoked
  THEN it returns exemptions as K8s-envelope JSON

---

### T4: Traces HTTP client, commands (recommendations show/apply/dismiss), and Policy TypedCRUD adapter

**Priority**: P1
**Effort**: Medium-Large
**Depends on**: T1
**Type**: task

Implement the traces subpackage: domain types (`Policy` with `ResourceIdentity`, `Recommendation`), HTTP client for `/adaptive-traces/api/v1/policies` (full CRUD) and `/adaptive-traces/api/v1/recommendations` endpoints, provider commands (`recommendations show`, `recommendations apply`, `recommendations dismiss`), and the `TypedCRUD[Policy]` adapter registration with `SchemaFromType[Policy]()` and example JSON. The Policy type MUST implement `GetResourceName()` and `SetResourceName()`. All destructive commands (`apply`, `dismiss`) MUST support `--dry-run`.

**Deliverables:**
- `internal/providers/adaptive/traces/types.go`
- `internal/providers/adaptive/traces/client.go`
- `internal/providers/adaptive/traces/client_test.go`
- `internal/providers/adaptive/traces/resource_adapter.go`
- `internal/providers/adaptive/traces/commands.go`

**Acceptance criteria:**
- GIVEN a traces HTTP client configured with valid credentials
  WHEN `ListPolicies(ctx)` is called
  THEN it returns a slice of `Policy` with all fields populated
- GIVEN a traces HTTP client configured with valid credentials
  WHEN `CreatePolicy(ctx, policy)` is called
  THEN it sends a POST and returns the created policy
- GIVEN a traces HTTP client configured with valid credentials
  WHEN `DeletePolicy(ctx, id)` is called
  THEN it sends a DELETE to the policies endpoint
- GIVEN a `Policy` value
  WHEN `GetResourceName()` is called
  THEN it returns the policy's ID
- GIVEN a `*Policy` value
  WHEN `SetResourceName("new-id")` is called
  THEN the policy's ID field is updated
- GIVEN the adaptive provider is registered
  WHEN `TypedRegistrations()` includes the Policy registration
  THEN it has a non-nil Schema and non-nil Example
- GIVEN `gcx adaptive traces recommendations show` is invoked
  WHEN recommendations exist
  THEN a formatted table is printed to stdout
- GIVEN `gcx adaptive traces recommendations apply rec-123 --dry-run` is invoked
  THEN the command prints what would be applied without POSTing
- GIVEN `gcx adaptive traces recommendations dismiss rec-123 --dry-run` is invoked
  THEN the command prints what would be dismissed without POSTing
- GIVEN the Policy adapter is registered
  WHEN `gcx resources get policies.v1alpha1.adaptive-traces.ext.grafana.app` is invoked
  THEN it returns policies as K8s-envelope JSON

---

## Wave 3: Integration Wiring

### T5: Wire Commands() and TypedRegistrations(), update provider_test, run make all

**Priority**: P1
**Effort**: Small
**Depends on**: T2, T3, T4
**Type**: chore

Wire the three signal subpackages into the provider: update `Commands()` to build the `gcx adaptive` command tree with `metrics`, `logs`, `traces` subcommands; update `TypedRegistrations()` to return Exemption and Policy registrations from the logs and traces subpackages. Add provider-level integration tests verifying the full command tree structure (16 leaf commands) and both adapter registrations. Run `GCX_AGENT_MODE=false make all` to verify build, lint, tests, and docs generation pass.

**Deliverables:**
- `internal/providers/adaptive/provider.go` (updated Commands + TypedRegistrations)
- `internal/providers/adaptive/provider_test.go` (updated with command tree + registration tests)

**Acceptance criteria:**
- GIVEN the adaptive provider is registered
  WHEN `Commands()` is called
  THEN it returns a single `*cobra.Command` with Use == "adaptive" containing subcommands "metrics", "logs", "traces"
- GIVEN the full command tree
  WHEN all leaf commands are enumerated
  THEN there are exactly 16 leaf commands matching the spec's command tree
- GIVEN the adaptive provider
  WHEN `TypedRegistrations()` is called
  THEN it returns exactly 2 registrations: one for Exemption (logs) and one for Policy (traces), each with non-nil Schema
- GIVEN the complete implementation
  WHEN `GCX_AGENT_MODE=false make all` is run
  THEN it exits 0 with no lint errors, all tests pass, and docs generate without drift
