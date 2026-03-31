---
type: feature-spec
title: "Adaptive Telemetry Provider"
status: done
beads_id: grafanactl-experiments-e6h.1
created: 2026-03-27
---

# Adaptive Telemetry Provider

## Problem Statement

Grafana Cloud offers three Adaptive Telemetry features — Adaptive Metrics, Adaptive Logs, and Adaptive Traces — that help users reduce observability costs by aggregating, sampling, or dropping low-value telemetry data. The existing `grafana-cloud-cli` exposes these as three independent top-level commands (`adaptive-metrics`, `adaptive-logs`, `adaptive-traces`) with duplicated auth logic, inconsistent verb naming, and no integration with gcx's unified resource pipeline (selectors, adapters, push/pull).

Users who manage cost optimization across all three signals must learn three separate command hierarchies with subtly different semantics. Resources that support full CRUD (log exemptions, trace policies) cannot participate in gcx's `get`, `push`, `pull`, `delete` resource commands because no adapters exist.

There is no current workaround within gcx — users must use the old `grafana-cloud-cli` binary or call the REST APIs directly.

## Scope

### In Scope

- Single `adaptive` provider registered via `init()` self-registration pattern
- Three signal subareas: `metrics`, `logs`, `traces` as subcommands under `gcx adaptive`
- Full command tree: 16 leaf commands (see FR-001 through FR-016)
- HTTP clients for all three signal APIs with shared Basic auth helper
- Two TypedCRUD adapter registrations: `Exemption` (adaptive-logs) and `Policy` (adaptive-traces)
- Auth via `LoadCloudConfig()` and GCOM stack lookup for instance URLs/IDs
- `show` verb for provider-only read-only collections (rules, recommendations, patterns)
- Table codecs (text/wide) for all `show` commands
- JSON Schema generation via `SchemaFromType[T]()` for Exemption and Policy
- Dry-run support for destructive operations (`sync`, `apply`)
- Unit tests for all HTTP clients and adapter registrations
- Parity with all non-dropped `grafana-cloud-cli` adaptive commands

### Out of Scope

- **`adaptive-traces insights` and `adaptive-traces tenants` commands** — dropped per ADR; these are untyped internal tooling not intended for external users
- **Adaptive Profiles** — separate signal, not part of this provider
- **Integration/e2e tests against live Grafana Cloud** — unit tests with HTTP mocks only
- **Table codecs for adapter resources** — adapter resources use the standard resource pipeline output (json/yaml/table via codec registry); no custom table codecs needed
- **Metrics rules adapter registration** — metrics rules use bulk-replace semantics (ETag-based POST), incompatible with individual CRUD adapter pattern
- **Metrics recommendations adapter registration** — read-only bulk collection, no individual identity
- **Logs patterns adapter registration** — bulk-replace semantics via POST array, no individual CRUD
- **Traces recommendations adapter registration** — read-only with action verbs (apply/dismiss), no individual CRUD

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Provider count | Single `adaptive` provider with `metrics`/`logs`/`traces` subareas | Shared auth, config, and patterns; reduces registration overhead; single `gcx providers list` entry | ADR |
| Adapter-eligible resources | Only Exemption and Policy (full CRUD) | TypedCRUD requires individual get/create/update/delete; bulk-replace and action-only resources do not fit | ADR |
| Verb for provider-only lists | `show` (not `list`) | CONSTITUTION.md verb mimicry prohibition: `list` is reserved for adapter-backed resources | ADR |
| Auth mechanism | `LoadCloudConfig()` → StackInfo → Basic auth `{instanceID}:{apToken}` | Matches fleet provider pattern; no provider-specific config keys needed | ADR |
| ConfigKeys return value | 6 cache keys (`{signal}-tenant-id/url`), all `Secret: false` | Declare cached GCOM values so they're visible in `gcx config view`; no user-configured auth keys | ADR + validator feedback |
| Metrics rules sync concurrency | ETag-based optimistic concurrency (GET ETag, POST with If-Match) | Existing API contract; prevents silent overwrites | Source API |
| Logs patterns apply semantics | Full array replacement via POST | Existing API contract; individual pattern mutation not supported | Source API |
| Dropped commands | `insights get/create`, `tenants list/create` | Untyped internal tooling; raw JSON in/out with no schema | ADR |
| GCOM caching | Cache tenant IDs/URLs in config at `cloud.{signal}-tenant-id`/`cloud.{signal}-tenant-url` | Avoids repeated GCOM lookups; cached values used on subsequent invocations | User feedback |
| Dry-run scope | All destructive operations (sync, apply, dismiss) | Consistency; all mutations should be previewable | User feedback |

## Functional Requirements

**Provider Registration**

- **FR-001**: The provider MUST self-register via `init()` using `providers.Register()` with name `"adaptive"` and short description `"Manage Grafana Cloud Adaptive Telemetry."`.
- **FR-002**: `ConfigKeys()` MUST return entries for all six cached GCOM values (`metrics-tenant-id`, `metrics-tenant-url`, `logs-tenant-id`, `logs-tenant-url`, `traces-tenant-id`, `traces-tenant-url`) with `Secret: false`. This ensures cached values are visible (not redacted) in `gcx config view`.
- **FR-003**: The provider MUST return two `TypedRegistration` entries from `TypedRegistrations()`: one for `adaptive-logs.ext.grafana.app/v1alpha1/Exemption` and one for `adaptive-traces.ext.grafana.app/v1alpha1/Policy`.

**Auth**

- **FR-004**: All HTTP clients MUST authenticate using Basic auth with credentials `{instanceID}:{apToken}` where `instanceID` is the signal-specific instance ID from GCOM StackInfo and `apToken` is the Grafana Cloud Access Policy token from cloud config.
- **FR-005**: Instance URLs and tenant IDs MUST be resolved from GCOM StackInfo fields: `HMInstancePromURL`/`HMInstancePromID` (metrics), `HLInstanceURL`/`HLInstanceID` (logs), `HTInstanceURL`/`HTInstanceID` (traces).
- **FR-005a**: Resolved instance URLs and tenant IDs MUST be cached in the config at paths `cloud.metrics-tenant-id`, `cloud.metrics-tenant-url`, `cloud.logs-tenant-id`, `cloud.logs-tenant-url`, `cloud.traces-tenant-id`, `cloud.traces-tenant-url` via `SaveProviderConfig` (or equivalent config write-back). Subsequent commands MUST use cached values when present, falling back to GCOM lookup only when cache is empty.
- **FR-006**: The HTTP client MUST use `providers.ExternalHTTPClient()` as the base transport.

**Metrics Commands**

- **FR-007**: `gcx adaptive metrics rules show` MUST list all aggregation rules from `GET {hmURL}/aggregations/rules` and display them via the output codec system.
- **FR-008**: `gcx adaptive metrics rules sync -f <file>` MUST read rules from a JSON/YAML file and replace all existing rules via POST with ETag-based optimistic concurrency.
- **FR-009**: `gcx adaptive metrics rules sync --from-recommendations` MUST fetch current recommendations, convert them to rules, and replace all existing rules via POST with ETag-based optimistic concurrency.
- **FR-010**: `gcx adaptive metrics recommendations show` MUST list all recommendations from `GET {hmURL}/aggregations/recommendations`.

**Logs Commands**

- **FR-011**: `gcx adaptive logs exemptions` MUST be backed by a TypedCRUD adapter supporting list, get, create, update, and delete operations against `{hlURL}/adaptive-logs/exemptions`.
- **FR-012**: `gcx adaptive logs patterns show` MUST list all patterns/recommendations from `GET {hlURL}/adaptive-logs/recommendations`.
- **FR-013**: `gcx adaptive logs patterns apply <substring>` MUST match patterns by case-insensitive substring, set `configured_drop_rate` to `recommended_drop_rate` (or a custom `--rate` value), and POST the full array. `--all` MUST apply to all patterns.

**Traces Commands**

- **FR-014**: `gcx adaptive traces policies` MUST be backed by a TypedCRUD adapter supporting list, get, create, update, and delete operations against `{htURL}/adaptive-traces/api/v1/policies`.
- **FR-015**: `gcx adaptive traces recommendations show` MUST list all recommendations from `GET {htURL}/adaptive-traces/api/v1/recommendations`.
- **FR-016**: `gcx adaptive traces recommendations apply <id>` MUST POST to the apply endpoint. `gcx adaptive traces recommendations dismiss <id>` MUST POST to the dismiss endpoint.

**Output**

- **FR-017**: All `show` commands MUST support `--output` flag with `json`, `yaml`, `text`, and `wide` formats via the codec registry.
- **FR-018**: All destructive commands MUST support `--dry-run` to preview the operation without executing it. This includes: `metrics rules sync`, `metrics recommendations apply`, `logs patterns apply`, `traces recommendations apply`, and `traces recommendations dismiss`.

**Types**

- **FR-019**: `Exemption` and `Policy` types MUST implement `ResourceIdentity` (GetResourceName/SetResourceName) for TypedCRUD compatibility.
- **FR-020**: `Exemption` MUST have a non-nil JSON Schema and Example registered via `TypedRegistration`.
- **FR-021**: `Policy` MUST have a non-nil JSON Schema and Example registered via `TypedRegistration`.

## Acceptance Criteria

**Provider lifecycle**

- GIVEN gcx is built with the adaptive provider package imported
  WHEN `gcx providers list` is executed
  THEN the output MUST include an entry with name `adaptive` and the description `Manage Grafana Cloud Adaptive Telemetry.`

- GIVEN gcx is built with the adaptive provider package imported
  WHEN the adapter registry is queried for `adaptive-logs.ext.grafana.app/v1alpha1/Exemption`
  THEN a Registration with non-nil Schema and non-nil Example MUST be returned

- GIVEN gcx is built with the adaptive provider package imported
  WHEN the adapter registry is queried for `adaptive-traces.ext.grafana.app/v1alpha1/Policy`
  THEN a Registration with non-nil Schema and non-nil Example MUST be returned

**Auth**

- GIVEN a valid cloud config with stack name and AP token
  WHEN any adaptive command is executed
  THEN the HTTP request MUST contain an `Authorization: Basic {base64({instanceID}:{apToken})}` header

- GIVEN a cloud config without a stack name or AP token
  WHEN any adaptive command is executed
  THEN the command MUST return a DetailedError with an actionable suggestion to configure cloud credentials

**Metrics rules**

- GIVEN a Grafana Cloud stack with existing aggregation rules
  WHEN `gcx adaptive metrics rules show` is executed
  THEN all rules MUST be listed in the selected output format

- GIVEN a valid rules JSON file
  WHEN `gcx adaptive metrics rules sync -f rules.json` is executed
  THEN the client MUST first GET the current ETag, then POST rules with `If-Match` header set to that ETag

- GIVEN the `--from-recommendations` flag is set
  WHEN `gcx adaptive metrics rules sync --from-recommendations` is executed
  THEN the client MUST fetch recommendations, convert each to a MetricRule, and POST them as the new rule set

- GIVEN the `--dry-run` flag is set
  WHEN `gcx adaptive metrics rules sync -f rules.json --dry-run` is executed
  THEN no POST request MUST be made and a dry-run report MUST be printed

- GIVEN neither `-f` nor `--from-recommendations` is provided
  WHEN `gcx adaptive metrics rules sync` is executed
  THEN the command MUST return an error stating one of the two flags is required

**Metrics recommendations**

- GIVEN a Grafana Cloud stack
  WHEN `gcx adaptive metrics recommendations show` is executed
  THEN all recommendations MUST be listed in the selected output format

- GIVEN no recommendations exist
  WHEN `gcx adaptive metrics recommendations show` is executed
  THEN an informational "No recommendations found" message MUST be printed

**Logs exemptions (adapter)**

- GIVEN a Grafana Cloud stack with existing exemptions
  WHEN `gcx adaptive logs exemptions list` is executed
  THEN all exemptions MUST be returned as TypedObject envelopes with apiVersion `adaptive-logs.ext.grafana.app/v1alpha1` and kind `Exemption`

- GIVEN a valid exemption YAML file
  WHEN `gcx adaptive logs exemptions push -f exemption.yaml` is executed
  THEN the adapter MUST create-or-update the exemption via the exemptions API

- GIVEN an existing exemption ID
  WHEN `gcx adaptive logs exemptions get <id>` is executed
  THEN the exemption MUST be returned as a TypedObject envelope

- GIVEN an existing exemption ID
  WHEN `gcx adaptive logs exemptions delete <id>` is executed
  THEN the exemption MUST be deleted via DELETE `{hlURL}/adaptive-logs/exemptions/{id}`

- GIVEN the `pull` subcommand
  WHEN `gcx adaptive logs exemptions pull` is executed
  THEN all exemptions MUST be exported to local files in the standard resource file layout

**Logs patterns**

- GIVEN a Grafana Cloud stack with existing patterns
  WHEN `gcx adaptive logs patterns show` is executed
  THEN all patterns MUST be listed in the selected output format

- GIVEN a pattern substring argument
  WHEN `gcx adaptive logs patterns apply "GET <*>"` is executed
  THEN only patterns whose text contains the substring (case-insensitive) MUST have their `configured_drop_rate` set to `recommended_drop_rate` and the full array MUST be POSTed

- GIVEN `--rate 50` is provided
  WHEN `gcx adaptive logs patterns apply "GET <*>" --rate 50` is executed
  THEN matching patterns MUST have `configured_drop_rate` set to 50 instead of the recommended rate

- GIVEN `--all` is provided
  WHEN `gcx adaptive logs patterns apply --all` is executed
  THEN all patterns MUST have their `configured_drop_rate` set to `recommended_drop_rate`

- GIVEN no substring and no `--all` flag
  WHEN `gcx adaptive logs patterns apply` is executed
  THEN the command MUST return an error

**Traces policies (adapter)**

- GIVEN a Grafana Cloud stack with existing policies
  WHEN `gcx adaptive traces policies list` is executed
  THEN all policies MUST be returned as TypedObject envelopes with apiVersion `adaptive-traces.ext.grafana.app/v1alpha1` and kind `Policy`

- GIVEN a valid policy YAML file
  WHEN `gcx adaptive traces policies push -f policy.yaml` is executed
  THEN the adapter MUST create-or-update the policy via the policies API

- GIVEN an existing policy ID
  WHEN `gcx adaptive traces policies get <id>` is executed
  THEN the policy MUST be returned as a TypedObject envelope

- GIVEN an existing policy ID
  WHEN `gcx adaptive traces policies delete <id>` is executed
  THEN the policy MUST be deleted via DELETE `{htURL}/adaptive-traces/api/v1/policies/{id}`

- GIVEN the `pull` subcommand
  WHEN `gcx adaptive traces policies pull` is executed
  THEN all policies MUST be exported to local files in the standard resource file layout

**Traces recommendations**

- GIVEN a Grafana Cloud stack
  WHEN `gcx adaptive traces recommendations show` is executed
  THEN all recommendations MUST be listed in the selected output format

- GIVEN a recommendation ID
  WHEN `gcx adaptive traces recommendations apply <id>` is executed
  THEN a POST MUST be made to the apply endpoint for that recommendation

- GIVEN a recommendation ID
  WHEN `gcx adaptive traces recommendations dismiss <id>` is executed
  THEN a POST MUST be made to the dismiss endpoint for that recommendation

## Negative Constraints

- **NC-001**: The provider MUST NOT require any user-configured provider-specific config keys for auth. Auth MUST be derived entirely from `LoadCloudConfig()`. The only declared `ConfigKeys()` entries are the auto-populated GCOM cache keys (tenant IDs and URLs), which users never need to set manually.
- **NC-002**: The provider MUST NOT expose `adaptive-traces insights` or `adaptive-traces tenants` commands.
- **NC-003**: Metrics rules `sync` MUST NOT proceed without ETag-based optimistic concurrency. If the GET-for-ETag fails, the sync MUST fail.
- **NC-004**: The `list` verb MUST NOT be used for provider-only read-only collections. These MUST use `show`.
- **NC-005**: Adapter registrations MUST NOT have nil Schema. `SchemaFromType[T]()` MUST be used.
- **NC-006**: The provider MUST NOT create adapter registrations for metrics rules, metrics recommendations, logs patterns, or traces recommendations. Only Exemption and Policy qualify.
- **NC-007**: Logs patterns `apply` MUST NOT mutate only the matched patterns — it MUST POST the full array (API contract requires full replacement).
- **NC-008**: HTTP clients MUST NOT embed credentials in URLs. Basic auth MUST be set via the `Authorization` header.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| ETag race condition on metrics rules sync under concurrent writers | Rules could be silently overwritten if ETag changes between GET and POST | Return clear error on 412 Precondition Failed; user retries. Document in command help. |
| GCOM stack lookup adds latency on first command invocation | User-perceived slowness on first use per context | Mitigated by FR-005a: cache tenant IDs/URLs in config; GCOM only called when cache is empty |
| Logs patterns full-array POST can reset rates for non-targeted patterns | Unintended side effects if another user modified patterns between GET and POST | Document the destructive nature; implement dry-run preview |
| Exemption/Policy API response envelope differences (logs wraps in `{"result": [...]}`, traces does not) | Incorrect deserialization if not handled per-signal | Each signal's HTTP client MUST handle its own response envelope format |
| Breaking changes to adaptive APIs (v1alpha1) | Client code becomes incompatible | Pin to v1alpha1; version is in the GVK; new versions get new adapter registrations |

## Open Questions

- [RESOLVED] One provider vs three: Single provider with three subareas chosen. Rationale: shared auth pattern, reduced config surface, single `providers list` entry.
- [RESOLVED] Which resources get adapters: Only Exemption and Policy (full individual CRUD). Bulk-replace and action-only resources remain provider commands with `show` verb.
- [RESOLVED] Verb naming for provider-only collections: `show` per CONSTITUTION.md.
- [RESOLVED] Dropped commands: `insights` and `tenants` dropped (untyped internal tooling).
- [RESOLVED] Caching GCOM StackInfo: Cache tenant IDs and URLs in config at `cloud.{signal}-tenant-id` / `cloud.{signal}-tenant-url`. Subsequent commands use cached values, falling back to GCOM lookup when empty.
- [DEFERRED] Metrics rules adapter registration if/when the API evolves to support individual CRUD.
