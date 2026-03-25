---
type: feature-tasks
title: "Codify CLI Design Principles in CONSTITUTION.md and Design Guide"
status: approved
spec: docs/specs/constitution-design-principles/spec.md
plan: docs/specs/constitution-design-principles/plan.md
created: 2026-03-25
---

# Implementation Tasks

## Dependency Graph

```
Wave 1 (parallel, docs-only):

  T1 (CONSTITUTION.md) ──┐
  T2 (design-guide.md) ──┼──→ Wave 2
  T3 (DESIGN.md)       ──┘

Wave 2 (code + audits):

  T4 (remove deprecation) ←── depends on T1
  T5 (ResourceAdapter audit) ←── depends on T2
  T6 (ConfigLoader audit) ←── depends on T2
```

## Wave 1: Documentation — Codify Design Principles

### T1: Add design principle sections to CONSTITUTION.md

**Priority**: P0
**Effort**: Small
**Depends on**: none
**Type**: chore
**Bead**: uxu.1

Add four new sections to CONSTITUTION.md after "Architecture Invariants" and before "Dependency Rules": CLI Grammar, Dual-Purpose Design, Push/Pull Philosophy, Provider Architecture. Add the ConfigLoader dependency rule to the existing "Dependency Rules" section. All section text MUST be copied exactly from ADR-001 at `docs/adrs/constitution-design-principles/001-codify-cli-design-principles.md`.

**Deliverables:**
- `CONSTITUTION.md` (updated — 9 sections total)

**Acceptance criteria:**
- GIVEN the current CONSTITUTION.md with 5 sections
  WHEN T1 is complete
  THEN CONSTITUTION.md contains 9 sections in order: Project Identity, Architecture Invariants, CLI Grammar, Dual-Purpose Design, Push/Pull Philosophy, Provider Architecture, Dependency Rules, Taste Rules, Quality Standards
- GIVEN the new CLI Grammar section
  WHEN a reader checks for command structure guidance
  THEN the section documents `$AREA $NOUN $VERB` structure, extension command nesting, and positional-vs-flag semantics
- GIVEN the new Provider Architecture section
  WHEN a reader checks whether provider commands are deprecated
  THEN the section explicitly states "Dual CRUD access paths are permanent" and "Neither path is deprecated; both are first-class"
- GIVEN the existing Dependency Rules section
  WHEN T1 is complete
  THEN the section includes a rule requiring `providers.ConfigLoader` for config and auth resolution with a rationale referencing consistent env var precedence, secret handling, and auth behavior
- GIVEN all new CONSTITUTION.md content
  WHEN compared against ADR-001 text
  THEN the section text matches exactly (FR-006)
- GIVEN the completed CONSTITUTION.md
  WHEN reviewed against NC-001
  THEN no implementation details, code examples, or `[ADOPT]`/`[PLANNED]` markers are present in any section

---

### T2: Add design principle sections to design-guide.md

**Priority**: P0
**Effort**: Medium
**Depends on**: none
**Type**: chore
**Bead**: uxu.2

Add six new or updated sections to `docs/reference/design-guide.md`. New sections: Codec Requirements by Command Type `[ADOPT]`, Mutation Command Output `[ADOPT]`, Pull Format Consistency `[ADOPT]`, Provider/Resources Output Consistency `[ADOPT]`, Provider ConfigLoader `[ADOPT]`. Updated section: TypedCRUD Pattern `[ADOPT → EVOLVE]`. All section content comes from ADR-001. New sections MUST follow the existing numbering scheme (sections 1-10 exist; new sections continue from 11 or are inserted at appropriate positions with renumbering). Each new section MUST use `[ADOPT]` status marker (or `[ADOPT → EVOLVE]` for TypedCRUD).

**Deliverables:**
- `docs/reference/design-guide.md` (updated — 6 new/updated sections)

**Acceptance criteria:**
- GIVEN design-guide.md with its current sections
  WHEN T2 is complete
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
- GIVEN all new design-guide.md sections
  WHEN reviewed against NC-002
  THEN no `[CURRENT]` status markers appear on any new section
- GIVEN the Mutation Command Output section
  WHEN reviewed against NC-007
  THEN the section is documentation only with `[ADOPT]` marker; no code implementation exists
- GIVEN the Pull Format Consistency section
  WHEN reviewed against NC-008
  THEN the section is documentation only with `[ADOPT]` marker; no code implementation exists

---

### T3: Add CONSTITUTION.md cross-references to DESIGN.md

**Priority**: P1
**Effort**: Small
**Depends on**: none
**Type**: chore
**Bead**: uxu.3

Add cross-references in `DESIGN.md` pointing to the new CONSTITUTION.md sections (CLI Grammar, Dual-Purpose Design, Push/Pull Philosophy, Provider Architecture). References are inserted into existing DESIGN.md sections — no new sections are added. The most natural placement is in the "Reference Documentation" section at the bottom where CONSTITUTION.md is already linked.

**Deliverables:**
- `DESIGN.md` (updated — cross-references added)

**Acceptance criteria:**
- GIVEN the current DESIGN.md
  WHEN T3 is complete
  THEN DESIGN.md contains cross-references to the new CONSTITUTION.md sections (CLI Grammar, Dual-Purpose Design, Push/Pull Philosophy, Provider Architecture)
- GIVEN the completed DESIGN.md
  WHEN reviewed against NC-003
  THEN no new sections or structural changes exist beyond the added cross-references

---

## Wave 2: Code Change and Compliance Audits

### T4: Remove deprecation warnings from alert, slo, and synth providers

**Priority**: P0
**Effort**: Small
**Depends on**: T1
**Type**: chore
**Bead**: uxu.6

Remove the `WarnDeprecated` and `IsCRUDCommand` calls from `PersistentPreRun` hooks in three provider files: `internal/providers/alert/provider.go`, `internal/providers/slo/provider.go`, `internal/providers/synth/provider.go`. After removing these call sites, verify no other callers of `WarnDeprecated` or `IsCRUDCommand` exist. If none remain, delete `internal/providers/deprecation.go` and `internal/providers/deprecation_test.go`. The synth provider's `PersistentPreRun` contains non-deprecation logic (root command propagation) that MUST be preserved (NC-004).

**Deliverables:**
- `internal/providers/alert/provider.go` (updated)
- `internal/providers/slo/provider.go` (updated)
- `internal/providers/synth/provider.go` (updated)
- `internal/providers/deprecation.go` (deleted, if no callers remain)
- `internal/providers/deprecation_test.go` (deleted, if no callers remain)

**Acceptance criteria:**
- GIVEN a user running `grafanactl slo definitions list`
  WHEN the command executes
  THEN no deprecation warning is printed to stderr
- GIVEN a user running `grafanactl synth checks list`
  WHEN the command executes
  THEN no deprecation warning is printed to stderr
- GIVEN a user running `grafanactl alert rules list`
  WHEN the command executes
  THEN no deprecation warning is printed to stderr
- GIVEN the `WarnDeprecated` function
  WHEN all callers have been removed
  THEN the function and its tests are deleted from the codebase
- GIVEN the synth provider's `PersistentPreRun` hook
  WHEN reviewed after T4
  THEN non-deprecation logic (root command propagation, config loading) is preserved intact (NC-004)
- GIVEN the completed code change
  WHEN `make all` is run with `GRAFANACTL_AGENT_MODE=false`
  THEN build, lint, and tests pass with no regressions

---

### T5: Audit all providers for ResourceAdapter compliance

**Priority**: P1
**Effort**: Medium
**Depends on**: T2
**Type**: chore
**Bead**: uxu.7

Audit all 8 providers (alert, fleet, incidents, k6, kg, oncall, slo, synth) for ResourceAdapter compliance. For each provider, determine whether CRUD commands (list, get, push, pull, delete) use `ResourceAdapter` via TypedCRUD for data access, or whether they use raw API clients directly. Distinguish between CRUD operations (which MUST use adapter per the newly codified design-guide.md section) and extension/operational commands (which may use raw clients). Record findings as bead notes — create one follow-up bead per non-compliant provider with a remediation plan describing what code changes achieve compliance.

**Deliverables:**
- Bead notes on uxu.7 documenting compliance status for all 8 providers
- One new follow-up bead per non-compliant provider (with remediation plan in description)

**Acceptance criteria:**
- GIVEN the audit task
  WHEN T5 is complete
  THEN an audit report exists (as bead notes on uxu.7) documenting adapter compliance for all 8 providers with status (compliant/non-compliant) and specific non-compliant code paths
- GIVEN a non-compliant provider in the audit
  WHEN the report is reviewed
  THEN a follow-up bead exists with a remediation plan describing what code changes achieve compliance
- GIVEN the audit process
  WHEN T5 is complete
  THEN no provider source code has been modified (NC-005)
- GIVEN the fleet provider
  WHEN audited for ResourceAdapter usage
  THEN the audit distinguishes between CRUD operations (adapter required) and extension/operational commands (raw client acceptable)

---

### T6: Audit all providers for ConfigLoader compliance

**Priority**: P1
**Effort**: Medium
**Depends on**: T2
**Type**: chore
**Bead**: uxu.8

Audit all 8 providers (alert, fleet, incidents, k6, kg, oncall, slo, synth) for ConfigLoader compliance. For each provider, determine whether it uses `providers.ConfigLoader` for config and auth resolution, or whether it has custom config loading logic (e.g., synth's SM-specific config). Record findings as bead notes — create one follow-up bead per non-compliant provider with a remediation plan that accounts for provider-specific requirements.

**Deliverables:**
- Bead notes on uxu.8 documenting compliance status for all 8 providers
- One new follow-up bead per non-compliant provider (with remediation plan in description)

**Acceptance criteria:**
- GIVEN the audit task
  WHEN T6 is complete
  THEN an audit report exists (as bead notes on uxu.8) documenting ConfigLoader compliance for all 8 providers with status (compliant/non-compliant) and any custom config loading logic identified
- GIVEN the synth provider's custom configLoader
  WHEN the remediation plan is written
  THEN the plan accounts for SM-specific config requirements (sm-url, sm-token, sm-metrics-datasource-uid) and describes how `providers.ConfigLoader` can be extended or composed to support them
- GIVEN the audit process
  WHEN T6 is complete
  THEN no provider source code has been modified (NC-005)
- GIVEN k6 provider (flagged in open questions as potentially non-compliant)
  WHEN audited for ConfigLoader usage
  THEN the audit verifies whether k6 uses `providers.ConfigLoader{}` directly and documents the finding
