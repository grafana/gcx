---
type: feature-plan
title: "Codify CLI Design Principles in CONSTITUTION.md and Design Guide"
status: approved
spec: docs/specs/constitution-design-principles/spec.md
created: 2026-03-25
---

# Architecture and Design Decisions

## Pipeline Architecture

This feature is documentation-first with one surgical code change. No new runtime behavior is introduced.

```
ADR-001 (source of truth for exact text)
    |
    v
+---------------------+     +---------------------+     +---------------------+
| CONSTITUTION.md     |     | design-guide.md     |     | DESIGN.md           |
| (invariants layer)  |     | (prescriptive UX)   |     | (cross-references)  |
| +4 new sections     |     | +6 new/updated       |     | +refs to new        |
| +1 dep rule addition|     |  sections [ADOPT]    |     |  CONSTITUTION        |
+---------------------+     +---------------------+     |  sections           |
                                                         +---------------------+
           |                          |
           v                          v
+-------------------------------------------------------------+
| Code change: remove WarnDeprecated from 3 providers         |
| (alert, slo, synth) — aligns code with "dual paths are      |
|  permanent" invariant codified above)                        |
+-------------------------------------------------------------+
           |
           v
+-------------------------------------------------------------+
| Audits: ResourceAdapter + ConfigLoader compliance            |
| (reports as bead notes — one follow-up bead per non-         |
|  compliant provider)                                         |
+-------------------------------------------------------------+
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Wave 1 is docs-only (T1, T2, T3 in parallel) | The three doc files (CONSTITUTION.md, design-guide.md, DESIGN.md) have no mutual dependencies; parallel execution minimizes calendar time. Traces to FR-001 through FR-014. |
| Wave 2 groups code change with audits | T4 (deprecation removal) depends on T1 completing because the constitutional "dual paths are permanent" invariant must be codified before the warnings are removed. T5/T6 audits reference the newly codified standards. Traces to FR-015 through FR-023. |
| ADR-001 is the single source of truth for section text | FR-006 requires exact text match from ADR. Implementors copy from `docs/adrs/constitution-design-principles/001-codify-cli-design-principles.md`, not from the spec. |
| Audit findings go to bead notes, not files | Resolved open question in spec. One follow-up bead per non-compliant provider keeps remediation work trackable and assignable. |
| WarnDeprecated function removal is conditional | FR-017: remove `deprecation.go` and `deprecation_test.go` only if no callers remain after removing the three provider call sites. `IsCRUDCommand` is co-located and follows the same rule. |
| DESIGN.md gets cross-references only, no structural changes | NC-003 prohibits new sections. Cross-references are inserted into existing sections (e.g., Vision, Detailed Architecture Documentation) where CONSTITUTION.md topics are already adjacent. |
| New design-guide.md sections use `[ADOPT]` exclusively (except TypedCRUD which uses `[ADOPT → EVOLVE]`) | NC-002 prohibits `[CURRENT]` on new sections since these patterns are not yet consistently applied. FR-013 requires following the existing marker convention. |

## Compatibility

| Category | Details |
|----------|---------|
| Unchanged | All existing CONSTITUTION.md sections (Project Identity, Architecture Invariants, Dependency Rules, Taste Rules, Quality Standards) retain their current content verbatim. Existing design-guide.md sections 1-10 and Appendix are unchanged. DESIGN.md structure is preserved. |
| Deprecated | `WarnDeprecated` function and `IsCRUDCommand` helper in `internal/providers/deprecation.go` are removed (no callers remain after T4). |
| New | Four CONSTITUTION.md sections (CLI Grammar, Dual-Purpose Design, Push/Pull Philosophy, Provider Architecture). One Dependency Rules addition (ConfigLoader rule). Six design-guide.md sections. Cross-references in DESIGN.md. |
