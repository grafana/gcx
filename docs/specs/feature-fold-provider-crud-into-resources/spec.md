---
type: feature-spec
title: "Fold Provider CRUD into Resources Subcommand"
status: done
beads_id: grafanactl-experiments-ew9
created: 2026-03-10
---

# Fold Provider CRUD into Resources Subcommand

## Problem Statement

Provider-backed resource types (SLO definitions, Synthetic Monitoring checks/probes, alerting rules/groups) have their own CRUD command trees at the CLI root level (`grafanactl slo definitions push`, `grafanactl synth checks pull`, `grafanactl alert rules list`), while native Grafana resources use a unified `grafanactl resources` subcommand (`grafanactl resources push dashboards`, `grafanactl resources pull folders`). This split creates two problems:

1. **Inconsistent mental model**: Users must learn two different command patterns for the same conceptual operations (list, get, push, pull, delete). Native resources use `grafanactl resources <verb> <type>`, while providers use `grafanactl <provider> <resource-type> <verb>`. Agent workflows must encode knowledge of which command tree a resource lives in.

2. **Fragmented discoverability**: `grafanactl resources list` does not surface provider-backed types, giving an incomplete picture of manageable resources. Users must know that SLO, Synth, and Alert resources exist as separate top-level commands.

The current workaround is documentation and skills that map each provider to its specific command. There is no programmatic way to enumerate all CRUD-capable resource types through a single entry point.

## Scope

### In Scope

- A `ResourceAdapter` interface that bridges existing provider REST clients to the resources pipeline (Selector -> Filter -> Resource -> dynamic client equivalent)
- Pseudo-discovery registration: provider resource types appear in the discovery registry alongside native K8s-style resources
- Unified CRUD routing: `grafanactl resources push slo/definitions`, `grafanactl resources pull synth/checks`, `grafanactl resources list alert/rules` (and all other verbs) work for provider-backed types
- Adapter layer that marshals provider typed structs to/from `unstructured.Unstructured` (extending the existing `ToResource`/`FromResource` pattern in SLO and Synth adapters)
- Config/auth delegation: the resources pipeline delegates provider-specific config loading (e.g., Synth's SM URL/token) to the provider adapter without requiring the standard `NamespacedRESTConfig` path
- Processor pipeline compatibility: `ManagerFields`, `ServerFields`, `Namespace` processors work correctly with provider-backed resources (or are explicitly skipped with documented rationale)
- Backward compatibility: existing top-level provider commands (`grafanactl slo ...`, `grafanactl synth ...`, `grafanactl alert ...`) continue to work, with deprecation notices pointing to the unified path
- `grafanactl resources edit <provider-type>/<name>` support: get-modify-put cycle using the adapter's Get and Update methods with provider-specific validation
- Provider-specific read-only commands (`slo definitions status`, `slo definitions timeline`, `synth checks status`) remain accessible only under the provider subcommand tree (they have no resources equivalent)

### Out of Scope

- **Removing provider top-level commands** -- Removal happens after the unified path lands on main, is tested for a few days, and skills are updated. Tracked as a follow-up, not part of this spec's implementation scope.
- **Converting provider REST APIs to actual K8s-style APIs** -- The providers will continue using their proprietary REST endpoints behind the adapter. True K8s API support for SLO/Synth is a Grafana-side change outside grafanactl's control.
- **New provider implementations** -- This spec covers the adapter pattern for the three existing providers (SLO, Synth, Alert). New providers will follow the same pattern but are not specified here.
- **`grafanactl resources serve` for provider types** -- `resources serve` is being moved to `dev serve` anyway; not relevant here.
- **`grafanactl resources validate` for provider types** -- Validation requires provider-specific schema knowledge not yet abstracted.

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Keep provider top-level commands alongside unified path | Deprecate but retain | Avoids a breaking change; gives users and agent workflows time to migrate. Deprecation warnings guide the transition. | UX tradeoff: discoverability vs. consistency |
| Use adapter pattern (not rewrite) | Adapter wrapping existing provider clients | Providers already have working REST clients, typed models, and adapter code (`ToResource`/`FromResource`). Rewriting would duplicate effort and risk regressions. | Codebase context: existing adapter.go files |
| Register provider types as pseudo-discovery entries | Hard-code provider descriptors in registry | Provider REST APIs do not expose K8s-style `/apis` discovery. Hard-coding descriptors (group, version, kind, plural) is the only viable path. | Architecture gap: Discovery Mismatch |
| Selector syntax uses existing `resource.group` format | `slo.slo.ext.grafana.app` or short alias `slo` | Keeps the selector parser unchanged; provider types register short aliases (slo, checks, probes, rules, groups) for usability. | Existing selector parser in `selector.go` |
| Provider-specific auth handled by adapter, not resources pipeline | Adapter implements a `ResourceClient` interface that encapsulates auth | Synth requires SM URL/token separate from Grafana creds. Forcing all auth through `NamespacedRESTConfig` would break Synth. The adapter loads its own config internally. | Config/Auth Heterogeneity gap |
| Provider-specific commands (status, timeline) stay under provider tree | No resources equivalent for non-CRUD operations | These commands have no counterpart in the resources model (they query metrics, render charts). Forcing them into resources would bloat the abstraction. | Scope clarity |

## Functional Requirements

- FR-001: The system MUST define a `ResourceAdapter` interface with methods: `List(ctx) ([]*Resource, error)`, `Get(ctx, name) (*Resource, error)`, `Create(ctx, *Resource) error`, `Update(ctx, *Resource) error`, `Delete(ctx, name) error`.

- FR-002: Each existing provider (SLO, Synth, Alert) MUST implement the `ResourceAdapter` interface by wrapping its existing REST client and using the existing `ToResource`/`FromResource` adapter functions.

- FR-003: The discovery registry MUST accept static descriptor registrations from providers, alongside its dynamic `/apis`-based discovery. Static descriptors MUST include group, version, kind, singular, plural, and short aliases.

- FR-004: The selector parser MUST resolve provider resource type short aliases (e.g., `slo` resolves to `SLO` kind in `slo.ext.grafana.app` group) through the same lookup path as native resource aliases.

- FR-005: `grafanactl resources list <provider-type>` MUST return provider resources formatted as `unstructured.Unstructured` objects with the same envelope structure (`apiVersion`, `kind`, `metadata`, `spec`) as native resources.

- FR-006: `grafanactl resources get <provider-type>/<name>` MUST return a single provider resource in the standard resource envelope.

- FR-007: `grafanactl resources push <provider-type>` MUST read resource files from disk using the existing `FSReader`, convert them from `unstructured.Unstructured` to provider-typed structs via the adapter, and create/update them via the provider REST client.

- FR-008: `grafanactl resources pull <provider-type>` MUST fetch resources from the provider REST API, convert them to `unstructured.Unstructured` via the adapter, and write them to disk using the existing `FSWriter`.

- FR-009: `grafanactl resources delete <provider-type>/<name>` MUST delete the named resource via the provider REST client.

- FR-010: The `NamespaceOverrider` processor MUST work with provider-backed resources by setting the namespace in the `unstructured.Unstructured` metadata. The adapter MUST read this namespace when converting back to provider-typed structs.

- FR-011: The `ManagerFieldsAppender` processor MUST work with provider-backed resources identically to native resources.

- FR-012: Existing top-level provider commands (`grafanactl slo`, `grafanactl synth`, `grafanactl alert`) MUST continue to function and MUST print a deprecation warning to stderr on each invocation, directing users to the equivalent `grafanactl resources` command.

- FR-013: `grafanactl resources list` (with no type argument) MUST include provider-backed resource types in its output when a provider is configured and reachable.

- FR-014: `grafanactl providers list` MUST continue to work and MUST indicate which providers are available as resource types.

- FR-015: The `ResourceAdapter` factory MUST handle provider-specific config loading internally. For SLO and Alert, it MUST use the standard `NamespacedRESTConfig`. For Synth, it MUST use the Synth-specific `LoadSMConfig` path.

- FR-016: The system MUST NOT require provider config to be present when operating on native resources only. Provider adapter initialization MUST be lazy -- triggered only when a provider resource type is selected by a command.

- FR-017: `grafanactl resources push` (no explicit type argument, reading from files) MUST auto-detect provider resource types from the `apiVersion`/`kind` fields in the YAML files and route to the correct adapter. This enables mixed pushes of native and provider resources from the same directory.

- FR-018: `grafanactl resources edit <provider-type>/<name>` MUST implement get-modify-put: fetch the resource via the adapter's Get method, open in `$EDITOR`, then update via the adapter's Update method. The adapter MUST validate the modified resource before submitting.

## Acceptance Criteria

- GIVEN a Grafana instance with SLO definitions
  WHEN the user runs `grafanactl resources list slo`
  THEN the output displays SLO definitions in the same tabular format as native resources (NAME, NAMESPACE, KIND, AGE columns)

- GIVEN a Grafana instance with SLO definitions
  WHEN the user runs `grafanactl resources get slo/<uuid>`
  THEN the output displays the SLO definition as a YAML-encoded `unstructured.Unstructured` object with `apiVersion: slo.ext.grafana.app/v1alpha1`, `kind: SLO`, `metadata`, and `spec` fields

- GIVEN SLO definition YAML files on disk
  WHEN the user runs `grafanactl resources push slo -p ./slo-defs/`
  THEN the SLO definitions are created or updated via the SLO REST API and a summary reports the count of pushed resources

- GIVEN a Grafana instance with SLO definitions
  WHEN the user runs `grafanactl resources pull slo -d ./output/`
  THEN SLO definitions are written to `./output/` as YAML files with the standard resource envelope

- GIVEN a Grafana instance with an SLO definition named `abc-123`
  WHEN the user runs `grafanactl resources delete slo/abc-123`
  THEN the SLO definition is deleted via the SLO REST API

- GIVEN a Grafana instance with Synthetic Monitoring configured
  WHEN the user runs `grafanactl resources list checks`
  THEN Synthetic Monitoring checks are listed in standard resource format

- GIVEN a Grafana instance with alerting rules configured
  WHEN the user runs `grafanactl resources list rules`
  THEN alert rules are listed in standard resource format

- GIVEN the user runs `grafanactl slo definitions list`
  WHEN the command executes
  THEN a deprecation warning is printed to stderr: "Warning: 'grafanactl slo definitions list' is deprecated, use 'grafanactl resources list slo' instead"
  AND the command still produces correct output

- GIVEN no provider config is set (no SM URL/token, no provider section in config)
  WHEN the user runs `grafanactl resources list dashboards`
  THEN the command succeeds without errors related to provider initialization

- GIVEN provider types are registered in the discovery registry
  WHEN the user runs `grafanactl resources list` (no arguments)
  THEN provider-backed resource types appear alongside native resource types in the output

- GIVEN an SLO definition YAML file with `metadata.namespace: foo`
  WHEN the user runs `grafanactl resources push slo -p ./` in a context with namespace `bar`
  THEN the `NamespaceOverrider` processor sets the namespace to `bar` and the SLO is pushed with namespace `bar`

- GIVEN an SLO definition YAML file
  WHEN the user runs `grafanactl resources push slo --omit-manager-fields -p ./`
  THEN the SLO is pushed without manager field annotations

- GIVEN an SLO definition exists on the server
  WHEN the user runs `grafanactl resources edit slo/<uuid>`
  THEN the resource is fetched via the adapter's Get method, opened in `$EDITOR`, and the modified version is submitted via the adapter's Update method

- GIVEN the `ResourceAdapter` interface exists
  WHEN a new provider is implemented
  THEN it can participate in the resources pipeline by implementing `ResourceAdapter` and registering a static descriptor, without modifying the resources command code

## Negative Constraints

- NEVER initialize provider adapters eagerly at startup. Provider config loading MUST be deferred until a provider resource type is actually selected by a command. Eager loading would break `grafanactl resources list dashboards` for users who have no provider config.

- NEVER expose provider-specific REST API error formats (e.g., SLO plugin JSON errors, SM API error bodies) directly to the user. The adapter MUST translate these into the standard grafanactl error format used by the resources pipeline.

- NEVER modify the existing `Selector`, `Filter`, or `Descriptor` types in ways that break existing native resource flows. Provider support MUST be additive.

- NEVER route provider-specific non-CRUD commands (status, timeline, chart) through the resources pipeline. These commands MUST remain under the provider subcommand tree.

- DO NOT require users to specify the full `slo.ext.grafana.app` group when short aliases are registered. The short alias `slo` MUST resolve correctly.

- NEVER store provider REST client instances as package-level globals. Each adapter instance MUST be created per-command-invocation with its own config and client lifecycle.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Type safety loss when converting provider typed structs through `unstructured.Unstructured` | Data corruption or silent field drops during marshal/unmarshal round-trips | Extend existing adapter round-trip tests (adapter_test.go) to cover all fields. Add property-based tests for marshal/unmarshal symmetry. |
| Synth auth model breaks resources pipeline assumptions | Synth commands fail because resources pipeline tries to load Grafana REST config instead of SM config | `ResourceAdapter` factory encapsulates config loading. Integration tests verify Synth adapter works with SM-only config. |
| Processor pipeline incompatibility with provider resources | `ManagerFieldsAppender` or `ServerFields` processor panics on provider resources missing expected annotations | Unit tests for each processor with provider-shaped `unstructured.Unstructured` objects. Processors MUST handle missing fields gracefully. |
| Deprecation warnings disrupt agent/CI workflows | Agents or scripts parsing stdout see unexpected stderr output | Deprecation warnings go to stderr only. `--json` output mode suppresses warnings. Agent mode (`--agent`) suppresses deprecation warnings. |
| Performance regression from lazy adapter initialization | First provider-type command is slower due to deferred config/client setup | Measure latency in integration tests. Cache adapter instances within a single command invocation. |
| Breaking existing `.claude/skills` that reference old command paths | Agent workflows fail after deprecation | Skills MUST be updated in the same PR wave. Deprecation period allows gradual migration. |

## Open Questions

- [RESOLVED]: What is the deprecation timeline? -- Merge to main → test for a few days → update skills → remove old commands in follow-up PR.
- [RESOLVED]: Should `grafanactl resources push` auto-detect provider types from YAML? -- Yes, added as FR-017.
- [RESOLVED]: Should the `edit` verb be supported? -- Yes, added as FR-018. Full get-modify-put cycle included in scope.
- [RESOLVED]: Should `grafanactl resources serve` support provider types? -- No, `resources serve` is being moved to `dev serve` anyway.
- [RESOLVED]: Why was this rejected in the original provider design? -- The original rejection was due to the discovery mismatch (providers use hardcoded REST endpoints, not K8s `/apis`) and data model incompatibility (typed structs vs. unstructured). The adapter pattern described in this spec resolves both issues without requiring providers to implement K8s-style APIs.
