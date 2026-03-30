# Five-Phase Pipeline Redesign for /migrate-provider

**Created**: 2026-03-27
**Status**: accepted
**Bead**: TBD
**Supersedes**: [001-three-stage-blackbox-verification](001-three-stage-blackbox-verification.md)

## Context

The `/migrate-provider` skill was redesigned in ADR-001 from a flat checklist
to a 3-stage pipeline (Audit → Build → Verify) with dual blackbox isolation
("sealed envelopes"). ADR-001 assumed migrations are mechanical translations
that don't need a Design stage.

The first complex migration (adaptive telemetry provider — metrics, logs, traces)
invalidated this assumption. Over the course of that migration, 13 design decisions
were recorded in a decision log. The structural/workflow decisions that should
feed back into the skill are:

- **D-001**: Compliance docs (CONSTITUTION, design-guide, provider-guide,
  discovery-guide) must be explicitly checked during design, not assumed known.
- **D-002**: Progressive design discovery (iterative brainstorming with user
  approval at each step) produces better results than producing all artifacts
  at once.
- **D-003**: Migrations must produce project-standard documentation artifacts
  (ADRs, spec docs), not skill-specific ones.
- **D-004**: A Design Discovery phase with human-in-the-loop brainstorming is
  needed before speccing — it caught the read-only adapter issue and the verb
  naming issue early.
- **D-005**: The skill's three custom audit artifacts (parity table, architectural
  mapping, verification plan) map cleanly to spec documents (spec.md + plan.md +
  tasks.md) and should be replaced by them.
- **D-006**: Sealed envelope isolation is theatrical — agents can read any file
  on disk. The spec format provides natural separation without artificial barriers.
- **D-007**: The consolidated pipeline (Phase 0–4) incorporates all learnings.
- **D-011**: Parallel wave execution works but the integration task must
  explicitly include import-cycle resolution and lint fixup.
- **D-012**: Smoke testing caught three bug classes invisible to unit tests
  (response envelope hallucination, auth method substitution, missing codecs).
  Smoke tests must be mandatory, not optional.

The decision: how to restructure the skill to incorporate these learnings while
remaining followable by autonomous agents.

## Decision

We will restructure `/migrate-provider` from a 3-stage pipeline with sealed
envelopes to a **fixed 5-phase pipeline**: Requirements Gathering → Design
Discovery → Spec Planning → Build → Verification. Every migration runs all
5 phases — simple providers move through them faster, but no phase is skipped.

### Pipeline Structure

```
Phase 0: Requirements Gathering (autonomous)
    │
    ▼
Phase 1: Design Discovery (interactive brainstorming → ADR)
    │
    ▼
Phase 2: Spec Planning (spec.md + plan.md + tasks.md)
    │
    ▼
Phase 3: Build (recipe-guided implementation)
    │
    ▼
Phase 4: Verification (spec compliance + smoke tests)
```

### Phase 0: Requirements Gathering

**Runs autonomously** — no user interaction needed. The agent:

1. Reads the grafana-cloud-cli source for the target provider (requires the
   user to have added the source directory via `/add-dir` or equivalent).
2. Reads project compliance docs: CONSTITUTION.md, `docs/reference/design-guide.md`,
   `docs/reference/provider-guide.md`, `docs/reference/provider-discovery-guide.md`.
3. Reads the closest existing provider pattern in `internal/providers/`.
4. Maps every grafana-cloud-cli subcommand to a proposed gcx equivalent.
5. Records which compliance rules apply (from each doc).

**Output**: Context bundle — source summary, parity mapping (working draft),
compliance notes. This is a working document, not a user-facing artifact.

**Gate**: None — proceeds directly to Phase 1.

### Phase 1: Design Discovery

**Interactive** — progressive disclosure with user approval at each stage.
Each stage produces a focused design proposal, gets feedback, and iterates
before the next stage begins.

| Stage | Focus | Key questions |
|-------|-------|---------------|
| 1A | CLI UX | Command tree, naming, `$AREA $NOUN $VERB` grammar compliance |
| 1B | Resource adapters | Which resources get adapters, GVK mapping, verb choice |
| 1C | Auth & config | ConfigKeys, ConfigLoader, env vars, GCOM/instance lookup |
| 1D | Architecture | Package layout, client construction, shared helpers |

For simple providers (obvious auth, 1-2 resources, established pattern),
stages 1A-1D may each be a single question-and-answer exchange. The phases
still run — they just converge quickly when there's nothing to decide.

**Output**: ADR in `docs/adrs/{slug}/` documenting all design decisions.

**Gate**: User approves the ADR before proceeding.

### Phase 2: Spec Planning

Produces project-standard spec documents. Uses `/plan-spec` if available,
otherwise the agent produces them manually following the same format:

| Document | Content | Maps to (old skill) |
|----------|---------|---------------------|
| `spec.md` | FRs, acceptance criteria (Given/When/Then), NCs, risks | Parity table |
| `plan.md` | Architecture, HTTP client reference, design decisions, test strategy | Architectural mapping |
| `tasks.md` | Dependency graph, waves, per-task deliverables + ACs, **verification tasks** | Verification plan |

Smoke test design lives in `tasks.md` as verification tasks. Each `show`/`list`
command gets a verification task requiring all four output formats (`-o json`,
`-o table`, `-o wide`, `-o yaml`). Each adapter gets a verification task
requiring `resources schemas` and `resources get` checks.

The `plan.md` MUST include an HTTP client reference section: endpoint table
(method/path/purpose/notes), auth helper signature, client construction pattern.
This prevents the response envelope hallucination bug (D-012).

**Output**: Approved spec package (spec.md + plan.md + tasks.md).

**Gate**: User approves the spec package before proceeding.

### Phase 3: Build

Uses `/build-spec` if available, otherwise follows `gcx-provider-recipe.md`
with agent team orchestration.

Key changes from the current skill:

1. **No sealed envelopes.** Builders receive the full spec package. The spec
   format provides natural separation — implementation details live in plan.md
   and implementation tasks, verification expectations live in spec.md ACs and
   verification tasks.

2. **Integration task scope is explicit.** The final integration task (wiring
   `Commands()`, `TypedRegistrations()`, blank import) must also include:
   - Import cycle resolution (for multi-subpackage providers)
   - Variable name collision fixes (from package aliasing)
   - `make lint` on all new files

3. **Builder prompts include deserialization rule.** "Do NOT infer response
   envelope shapes. Copy deserialization code verbatim from the grafana-cloud-cli
   source. If the source does `json.Unmarshal(body, &slice)`, the new client
   must do the same."

**Gate**: `GCX_AGENT_MODE=false make all` passes.

### Phase 4: Verification

Structured verification with mandatory smoke tests.

```
4A: make all (unit tests + lint + build)
4B: Smoke tests — MANDATORY for each show/list command:
    ├── -o json   (verify deserialization works)
    ├── -o table  (verify table codec registered)
    ├── -o wide   (verify wide codec registered)
    └── -o yaml   (verify yaml round-trip)
4C: Adapter smoke — MANDATORY for each TypedCRUD:
    ├── resources schemas (verify registration visible)
    └── resources get (verify envelope + deserialization)
4D: Spec compliance — verify each AC from spec.md
4E: Recipe update — status tracker entry + gotchas (mandatory)
```

The comparison report template remains unchanged — it's already well-structured.

**Gate**: User approves comparison report. All discrepancies must be justified
or fixed.

### What changes from the current skill

| Current (3-stage) | New (5-phase) | Rationale |
|--------------------|---------------|-----------|
| No requirements gathering phase | Phase 0: autonomous context gathering | Separates research from design (D-007) |
| No design phase ("migrations don't need design") | Phase 1: progressive brainstorming → ADR | Adaptive migration proved design is needed (D-004) |
| Three custom audit artifacts | Spec documents (spec.md + plan.md + tasks.md) | Project-standard format, reusable by /build-spec (D-005) |
| Sealed envelopes (Build can't see tests) | Open spec — builders see full package | Isolation is theatrical, spec provides natural separation (D-006) |
| Smoke tests in Verify stage only | Smoke tests designed in Phase 2, executed in Phase 4 | Catches wire format and auth bugs that unit tests miss (D-012) |
| No compliance doc checking | Phase 0 reads CONSTITUTION + 3 reference docs | Prevents grammar violations and pattern drift (D-001) |
| Monolithic artifact production | Progressive disclosure per design aspect | Catches misalignment early (D-002) |

### What stays the same

- **Recipe remains the mechanical source of truth.** `gcx-provider-recipe.md`
  is referenced in Phase 3 for implementation steps. It's updated in Phase 4
  with new gotchas and status tracker entries.
- **Red flags table.** The "stop and check" patterns remain valuable. Adapt
  to the new phase structure (e.g., "copying cloud CLI client verbatim" is
  still a red flag in Phase 3).
- **File ownership table.** Useful for agent team parallelism in Phase 3.
- **Comparison report template.** Already well-structured for Phase 4.
- **`GCX_AGENT_MODE=false make all`** as the build gate.

## Rejected Alternatives

### B: Adaptive Pipeline with Complexity Gate

Phase 0 produces a complexity score; "simple" providers skip Phase 1 (Design
Discovery) and go straight to Phase 2 (Spec). This saves time for mechanical
ports.

**Rejected because:**
- The complexity gate introduces a decision point that agents might game
  ("I'll score it simple to skip design").
- Two paths through the skill doubles the instruction surface.
- A "simple" provider might hit unexpected design issues mid-spec with no
  design infrastructure to fall back on.
- The overhead of Phase 1 for simple providers is low — each brainstorming
  stage converges in a single exchange when there's nothing to decide.

### Current 3-Stage with Sealed Envelopes (status quo)

Keep the Audit → Build → Verify structure with dual blackbox isolation.

**Rejected because:**
- Sealed envelopes are not enforceable (agents can read any file on disk) — the
  isolation is theatrical (D-006).
- The custom audit artifacts are redundant with the project's spec format (D-005).
- No design phase means complex providers require ad-hoc design discovery
  that the skill doesn't support (D-004).
- Compliance doc checking is not mandated (D-001).

## Consequences

### Positive

- Every migration produces project-standard artifacts (ADR + spec docs) that
  are reusable by other tools and readable by future maintainers.
- Progressive brainstorming catches design issues before they're baked into
  specs or code.
- Mandatory smoke tests prevent the three bug classes discovered in D-012
  (envelope hallucination, auth substitution, missing codecs).
- Single path through the skill — agents can't skip phases or choose between
  paths.
- Phase 0 context gathering is reusable — the bundle feeds any downstream phase.

### Negative

- Every migration pays the cost of all 5 phases, even trivial ones. Mitigated
  by Phase 1 converging quickly for simple providers.
- Supersedes ADR-001, which was only recently written. The current skill
  structure has been used for 6 provider ports — changing it requires updating
  all reference materials.
- More phases = more gates = more user interaction required. Mitigated by
  Phase 0 being fully autonomous and Phase 1 being quick for simple providers.

### Follow-up

- Rewrite SKILL.md to implement the 5-phase pipeline.
- Update templates: replace audit artifact templates with spec guidance;
  update spawn prompts for the new Phase 3 structure.
- Update `gcx-provider-recipe.md` to reference the new phase structure.
- Retire the sealed envelope concept from all skill materials.
- Lift implementation-specific patterns from D-008 (auth subpackage), D-009
  (variable shadowing), D-010 (lint checklist), D-013 (debug logging) to
  CONSTITUTION.md and/or `docs/reference/design-guide.md`.
- First validation: next provider port from the migration epic.
