---
type: feature-spec
title: "Codify CLI Design Principles in CONSTITUTION.md and Design Guide"
status: done
beads_id: gcx-experiments-uxu
created: 2026-03-25
---

# Codify CLI Design Principles in CONSTITUTION.md and Design Guide

## Problem Statement

gcx's core design principles -- CLI grammar, dual-purpose human/agent design, push/pull philosophy, provider architecture invariants, and dependency rules -- exist in code and developers' heads but are not codified in the project's governance documents. This causes design drift, especially from agentic contributors who lack institutional context. Three providers emit misleading deprecation warnings about dual CRUD paths that are actually permanent. One provider (synth) has custom config/auth loading that bypasses `providers.ConfigLoader`, creating inconsistent env var precedence and auth behavior.

The current workaround is PR review by maintainers who hold the design principles in their heads, which does not scale to agentic contributors.

## Scope

### In Scope

- **uxu.1**: Add four new sections to `CONSTITUTION.md` (CLI Grammar, Dual-Purpose Design, Push/Pull Philosophy, Provider Architecture) and one addition to the existing Dependency Rules section (ConfigLoader requirement)
- **uxu.2**: Add six new or updated sections to `docs/reference/design-guide.md` (Codec Requirements by Command Type, Mutation Command Output, Pull Format Consistency, Provider/Resources Output Consistency, TypedCRUD status update, ConfigLoader Requirement)
- **uxu.3**: Add cross-references in `DESIGN.md` to the new CONSTITUTION.md sections
- **uxu.6**: Remove deprecation warnings from provider CRUD commands in alert, slo, and synth providers, reflecting the constitutional decision that dual CRUD access paths are permanent
- **uxu.7**: Audit all providers for ResourceAdapter compliance -- CRUD commands MUST use `ResourceAdapter` (via TypedCRUD) for data access, not raw API clients; produce an audit report and remediation plan for non-compliant providers
- **uxu.8**: Audit all providers for ConfigLoader compliance -- all providers MUST use `providers.ConfigLoader` for config and auth resolution; produce an audit report and remediation plan for non-compliant providers

### Out of Scope

- **uxu.4 (Mutation summary table codec)**: Implementing the actual structured summary output for push/pull/delete commands. The design-guide will document the spec; implementation is a separate feature.
- **uxu.5 (--format flag for pull)**: Implementing the `--format` flag on pull commands. The design-guide will document the spec; implementation is a separate feature.
- **Remediation of non-compliant providers**: The audits (uxu.7, uxu.8) produce findings and remediation plans. Actual code changes to fix non-compliant providers (e.g., migrating synth's custom configLoader to `providers.ConfigLoader`) are follow-up work tracked as separate tasks.
- **New codec implementations**: No new codecs, formatters, or output infrastructure is built in this feature.
- **Changes to `providers.ConfigLoader` itself**: The ConfigLoader interface and implementation are not modified.

## Key Decisions

| Decision | Chosen | Rationale | Source |
|----------|--------|-----------|--------|
| Dual CRUD paths are permanent | Both provider commands and generic `resources` commands are first-class | Removing either path loses value: provider commands lose domain-rich table output, generic commands lose push/pull pipeline integration | ADR-001 Provider Architecture section |
| JSON/YAML identity enforced via adapter, not tests | Provider CRUD commands MUST use ResourceAdapter for data access | Structural enforcement (same code path) is more reliable than test-based enforcement (comparing outputs) | ADR-001 Provider/Resources Output Consistency |
| TypedCRUD is transitional bridge, not target state | Status changed from `[ADOPT]` to `[ADOPT → EVOLVE]` | Domain types will eventually implement K8s interfaces directly, eliminating the bridge | ADR-001 TypedCRUD section |
| ConfigLoader is mandatory for all providers | All providers MUST use `providers.ConfigLoader` | Ensures consistent env var precedence, secret handling, and auth behavior | ADR-001 Dependency Rules Addition |
| Deprecation warnings removed, not softened | Remove entirely rather than rewording | The dual-path decision is constitutional; warnings contradict the invariant | ADR-001 Follow-Up Work item 6 |
| Audits produce reports, not code fixes | uxu.7 and uxu.8 are audit tasks that document findings | Fixing non-compliant providers requires provider-specific design decisions (e.g., synth's SM-specific config) that deserve separate tasks | ADR-001 Follow-Up Work items 7, 8 |

## Functional Requirements

**CONSTITUTION.md updates (uxu.1)**

- **FR-001**: CONSTITUTION.md MUST contain a "CLI Grammar" section after "Architecture Invariants" documenting: (a) `$AREA $NOUN $VERB` command structure, (b) extension commands nest under resource types, (c) positional arguments are subjects / flags are modifiers.
- **FR-002**: CONSTITUTION.md MUST contain a "Dual-Purpose Design" section documenting: (a) every command serves humans and agents, (b) all output goes through the codec system, (c) default output is proportional to what is actionable, (d) STDOUT is the result / STDERR is the diagnostic.
- **FR-003**: CONSTITUTION.md MUST contain a "Push/Pull Philosophy" section documenting: (a) local manifests are clean/portable/environment-agnostic, (b) three workflows / one pipeline, (c) folder-before-resource ordering on push.
- **FR-004**: CONSTITUTION.md MUST contain a "Provider Architecture" section documenting: (a) dual CRUD access paths are permanent, (b) JSON/YAML output is identical between both paths, (c) typed resource trajectory toward K8s interface compliance.
- **FR-005**: The existing "Dependency Rules" section in CONSTITUTION.md MUST include a rule stating that provider implementations MUST use `providers.ConfigLoader` for config and auth resolution.
- **FR-006**: All new CONSTITUTION.md section content MUST match the exact text specified in ADR-001 (sections "CLI Grammar", "Dual-Purpose Design", "Push/Pull Philosophy", "Provider Architecture", "Dependency Rules Addition").

**design-guide.md updates (uxu.2)**

- **FR-007**: design-guide.md MUST contain a "Codec Requirements by Command Type" section tagged `[ADOPT]` with a table specifying required codecs per command type (CRUD data, CRUD mutation, extension).
- **FR-008**: design-guide.md MUST contain a "Mutation Command Output" section tagged `[ADOPT]` specifying: (a) summary table format for STDOUT, (b) failure detail format for STDERR, (c) JSON summary shape, (d) rules for success counting / failure enumeration / skip threshold.
- **FR-009**: design-guide.md MUST contain a "Pull Format Consistency" section tagged `[ADOPT]` specifying the `--format` flag behavior and file naming convention.
- **FR-010**: design-guide.md MUST contain a "Provider / Resources Output Consistency" section tagged `[ADOPT]` requiring provider CRUD commands to use `ResourceAdapter` via TypedCRUD.
- **FR-011**: design-guide.md MUST contain an updated "TypedCRUD Pattern" section with status `[ADOPT → EVOLVE]` documenting current requirement and K8s interface trajectory.
- **FR-012**: design-guide.md MUST contain a "Provider ConfigLoader" section tagged `[ADOPT]` with explicit "do not" rules (no importing cmd config, no custom flag binding, no independent HTTP clients, no hardcoded env var names).
- **FR-013**: All new design-guide.md sections MUST use the existing status marker convention (`[CURRENT]`, `[ADOPT]`, `[PLANNED]`) and follow the existing section numbering scheme.

**DESIGN.md updates (uxu.3)**

- **FR-014**: DESIGN.md MUST add cross-references to the new CONSTITUTION.md sections. No structural changes to DESIGN.md beyond adding references.

**Deprecation warning removal (uxu.6)**

- **FR-015**: The `WarnDeprecated` calls in `internal/providers/alert/provider.go`, `internal/providers/slo/provider.go`, and `internal/providers/synth/provider.go` MUST be removed.
- **FR-016**: The `PersistentPreRun` hooks that call `WarnDeprecated` in the affected providers MUST be removed or simplified to no longer emit deprecation warnings for CRUD commands.
- **FR-017**: The `WarnDeprecated` function in `internal/providers/deprecation.go` and its tests in `internal/providers/deprecation_test.go` MUST be removed if no remaining callers exist, or retained if other callers remain.

**ResourceAdapter compliance audit (uxu.7)**

- **FR-018**: An audit report MUST be produced documenting, for each provider, whether its CRUD commands use `ResourceAdapter` (via TypedCRUD) for data access. The report MUST list each provider, its compliance status, and specific non-compliant code paths.
- **FR-019**: The audit MUST cover all providers: alert, fleet, incidents, k6, kg, oncall, slo, synth.
- **FR-020**: For non-compliant providers, the audit MUST include a remediation plan describing what changes are needed to achieve compliance.

**ConfigLoader compliance audit (uxu.8)**

- **FR-021**: An audit report MUST be produced documenting, for each provider, whether it uses `providers.ConfigLoader` for config and auth resolution. The report MUST identify any custom config loading logic.
- **FR-022**: The audit MUST cover all providers: alert, fleet, incidents, k6, kg, oncall, slo, synth.
- **FR-023**: For non-compliant providers, the audit MUST include a remediation plan that accounts for provider-specific requirements (e.g., synth's SM-specific config keys).

## Acceptance Criteria

**CONSTITUTION.md (uxu.1)**

- GIVEN the current CONSTITUTION.md with 5 sections
  WHEN uxu.1 is complete
  THEN CONSTITUTION.md contains 9 sections: Project Identity, Architecture Invariants, CLI Grammar, Dual-Purpose Design, Push/Pull Philosophy, Provider Architecture, Dependency Rules (with ConfigLoader rule added), Taste Rules, Quality Standards

- GIVEN the new CLI Grammar section
  WHEN a reader checks for command structure guidance
  THEN the section documents `$AREA $NOUN $VERB` structure, extension command nesting, and positional-vs-flag semantics

- GIVEN the new Provider Architecture section
  WHEN a reader checks whether provider commands are deprecated
  THEN the section explicitly states "Dual CRUD access paths are permanent" and "Neither path is deprecated; both are first-class"

- GIVEN the existing Dependency Rules section
  WHEN uxu.1 is complete
  THEN the section includes a rule requiring `providers.ConfigLoader` for config and auth resolution with a rationale referencing consistent env var precedence, secret handling, and auth behavior

**design-guide.md (uxu.2)**

- GIVEN design-guide.md with its current sections
  WHEN uxu.2 is complete
  THEN six new or updated sections exist: Codec Requirements by Command Type `[ADOPT]`, Mutation Command Output `[ADOPT]`, Pull Format Consistency `[ADOPT]`, Provider/Resources Output Consistency `[ADOPT]`, TypedCRUD Pattern `[ADOPT → EVOLVE]`, Provider ConfigLoader `[ADOPT]`

- GIVEN the new Codec Requirements section
  WHEN a reader looks up required codecs for a CRUD data command
  THEN the table shows: text (Required, default), wide (Required), json (Built-in), yaml (Built-in)

- GIVEN the new Mutation Command Output section
  WHEN a reader looks up the summary table format
  THEN the section specifies columns RESOURCE KIND | TOTAL | SUCCEEDED | SKIPPED | FAILED for STDOUT and RESOURCE | ERROR for STDERR

- GIVEN the new ConfigLoader Requirement section
  WHEN a reader checks what is prohibited
  THEN the section lists four explicit prohibitions: importing cmd config, custom flag binding for --config/--context, independent HTTP client construction, hardcoded env var names

**DESIGN.md (uxu.3)**

- GIVEN the current DESIGN.md
  WHEN uxu.3 is complete
  THEN DESIGN.md contains cross-references to the new CONSTITUTION.md sections (CLI Grammar, Dual-Purpose Design, Push/Pull Philosophy, Provider Architecture)

**Deprecation warning removal (uxu.6)**

- GIVEN a user running `gcx slo definitions list`
  WHEN the command executes
  THEN no deprecation warning is printed to stderr

- GIVEN a user running `gcx synth checks list`
  WHEN the command executes
  THEN no deprecation warning is printed to stderr

- GIVEN a user running `gcx alert rules list`
  WHEN the command executes
  THEN no deprecation warning is printed to stderr

- GIVEN the `WarnDeprecated` function
  WHEN all callers have been removed
  THEN the function and its tests are deleted from the codebase

**ResourceAdapter audit (uxu.7)**

- GIVEN the audit task
  WHEN uxu.7 is complete
  THEN an audit report exists documenting adapter compliance for all 8 providers with status (compliant/non-compliant) and specific non-compliant code paths

- GIVEN a non-compliant provider in the audit
  WHEN the report is reviewed
  THEN a remediation plan exists describing what code changes achieve compliance

**ConfigLoader audit (uxu.8)**

- GIVEN the audit task
  WHEN uxu.8 is complete
  THEN an audit report exists documenting ConfigLoader compliance for all 8 providers with status (compliant/non-compliant) and any custom config loading logic identified

- GIVEN the synth provider's custom configLoader
  WHEN the remediation plan is written
  THEN the plan accounts for SM-specific config requirements (sm-url, sm-token, sm-metrics-datasource-uid) and describes how `providers.ConfigLoader` can be extended or composed to support them

## Negative Constraints

- **NC-001**: CONSTITUTION.md MUST NOT contain implementation details, code examples, or `[ADOPT]`/`[PLANNED]` markers. It is invariants only -- "what you must not break". Implementation guidance belongs in design-guide.md.
- **NC-002**: design-guide.md new sections MUST NOT use `[CURRENT]` status markers. All new sections are either `[ADOPT]` or `[ADOPT → EVOLVE]` since they describe patterns not yet consistently applied.
- **NC-003**: DESIGN.md MUST NOT gain new sections or structural changes. Only cross-references to CONSTITUTION.md sections are added.
- **NC-004**: The deprecation warning removal (uxu.6) MUST NOT remove non-deprecation `PersistentPreRun` logic (e.g., root command propagation in synth's `PersistentPreRun`).
- **NC-005**: The audits (uxu.7, uxu.8) MUST NOT modify provider source code. They produce reports only.
- **NC-006**: No new CONSTITUTION.md content MUST contradict existing Architecture Invariants or Dependency Rules.
- **NC-007**: The mutation summary output spec in design-guide.md MUST NOT be implemented as code in this feature. It is documented as `[ADOPT]` for future implementation.
- **NC-008**: The pull format consistency spec in design-guide.md MUST NOT be implemented as code in this feature. It is documented as `[ADOPT]` for future implementation.

## Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Synth provider's custom configLoader handles SM-specific config that `providers.ConfigLoader` may not support | HIGH -- remediation plan may require ConfigLoader extension, blocking synth compliance | Audit (uxu.8) MUST document the specific SM-config requirements and propose a composition/extension approach rather than assuming drop-in replacement |
| Removing `WarnDeprecated` may confuse users who expect migration guidance | LOW -- the warnings were misleading since dual paths are permanent | CONSTITUTION.md explicitly documents dual paths as permanent; design-guide.md documents the rationale |
| Fleet provider may intentionally lack full ResourceAdapter coverage for operational commands | MEDIUM -- audit may flag intentional design as non-compliant | Audit (uxu.7) MUST distinguish between CRUD operations (which MUST use adapter) and extension/operational commands (which may use raw clients) |
| New CONSTITUTION.md sections may conflict with undocumented existing practices | MEDIUM -- may require code changes to align with newly codified invariants | All new sections are derived from existing code patterns documented in ADR-001; audits (uxu.7, uxu.8) will surface any conflicts |
| ADR section content becomes stale if design-guide.md sections get renumbered | LOW -- cross-references break | Use section titles (not numbers) for cross-references from CONSTITUTION.md to design-guide.md |

## Open Questions

- [RESOLVED] **Is k6 provider ConfigLoader-non-compliant?** The initial exploration data lists k6 as having a custom ConfigLoader. The spec-planner verified that k6 uses `providers.ConfigLoader{}` directly. k6 is ConfigLoader-compliant. The uxu.8 audit MUST verify this finding.
- [RESOLVED] **Does fleet provider have ResourceAdapter?** The spec-planner verified that fleet registers adapters for Pipeline and Collector resources via `adapter.Register`. Fleet appears ResourceAdapter-compliant. The uxu.7 audit MUST verify this finding.
- [RESOLVED] **Where should audit reports be stored?** Audit findings are recorded as bead notes/descriptions on new follow-up beads — one per non-compliant provider. No separate audit report files.
- [RESOLVED] **Should `providers.ConfigLoader` be extended with an interface for provider-specific config?** Yes — tracked as a separate follow-up bead under the architecture epic. The synth provider needs SM-specific config (sm-url, sm-token); a composition pattern (ConfigLoader + provider-specific extension) is the preferred approach.
