# Three-Stage Skill Structure with Dual Blackbox Isolation

**Created**: 2026-03-24
**Status**: proposed
**Bead**: grafanactl-experiments-zn8
**Supersedes**: none

## Context

The `/migrate-provider` skill needs a rewrite to match the orchestration
quality of `/add-provider`. The current skill is a flat checklist bolted onto
`provider-migration-recipe.md` — agents skim it, skip verification gates, produce
incomplete ports, and miss output format compliance.

Key problems observed during incidents, kg, and fleet migrations:
- No human gates → agents declared "done" without structured smoke diffs
- Flat checklist → agents cherry-picked steps instead of following phases
- No architectural mapping → agents copied cloud CLI patterns verbatim instead of
  adapting to grafanactl's framework (Options pattern, TypedCRUD[T], Processor
  pipeline, ResourceAdapter)
- Verification was an afterthought → context leaked between implementation
  and verification, making smoke tests confirm what the agent believed it
  built rather than independently verifying behavior

The decision: how to structure the rewritten skill — specifically, how many
stages, where to place gates, and how to prevent context leakage between
implementation and verification.

## Decision

We will use a **3-stage structure: Audit → Build → Verify** with **dual
blackbox isolation**: Build sees only the spec (not the verification plan),
and Verify sees only the verification plan (not the implementation details).

### Stage Structure

```
Audit ──gate──> Build ──gate──> Verify
  │                │                │
  ├─ parity table ─┘                │
  ├─ arch mapping ─┘                │
  │                                 │
  └─ verif plan ────────────────────┘
       (sealed from Build)    (sealed from impl)
```

Audit produces two sealed envelopes:
- **Build envelope**: parity table + architectural mapping + recipe reference
- **Verify envelope**: verification plan (test list + smoke commands + pass criteria)

Build never sees the verification plan. Verify never sees implementation
decisions. This is stage-level TDD: the spec and tests are written together
(Audit), then implementation (Build) and checking (Verify) run independently.

### Stage 1: Audit (brain)

Produces three artifacts before any code is written:

1. **Parity table** — maps every cloud CLI subcommand to a grafanactl equivalent
   with status (Implemented / Deferred / N/A) and justification.

2. **Architectural mapping** — maps cloud CLI patterns to grafanactl patterns:
   - Cloud CLI flat client → TypedCRUD[T] adapter with ToResource/FromResource
   - Cloud CLI CLI flags → Options struct + `setup(flags)` + `Validate()`
   - Cloud CLI output formatting → codec registry (table/wide/json/yaml) with
     K8s envelope wrapping via ToResource
   - Cloud CLI types → Go structs with `omitzero` for struct fields, exported
     codecs for `_test` package access
   - Cloud CLI provider registration → `adapter.Register()` in `init()` +
     blank import in `root/command.go`

3. **Verification plan** — concrete list of:
   - **Automated tests** to write (client httptest, adapter round-trip,
     TypedCRUD interface compliance)
   - **Smoke test commands** to run against a live instance (list/get/create
     for each resource, structured jq diffs against the cloud CLI, format checks for
     all four output modes)
   - **Build gates** (`GRAFANACTL_AGENT_MODE=false make all` at specified
     checkpoints)

**Gate**: User reviews and approves all three artifacts.

### Stage 2: Build (hands) — blackbox from verification

Receives **only** the Build envelope: parity table, architectural mapping,
and recipe reference. Does **not** see the verification plan.

Implements the provider following `provider-migration-recipe.md`, shaped by the
architectural mapping from Audit. Internal phases match the recipe (types →
client → adapter → resource_adapter → provider → commands) with `make lint`
checkpoints between phases. No design decisions happen here — all decisions
were made in Audit.

The builder writes unit tests based on the **requirements** (parity table +
arch mapping), not based on knowledge of what smoke tests will be run. This
prevents:
- Overfitting implementation to pass specific smoke checks
- Cutting corners on internals because "the smoke test only checks X"
- Writing unit tests that are tautological with the smoke test plan

**Gate**: `GRAFANACTL_AGENT_MODE=false make all` passes.

### Stage 3: Verify (inspector)

Executes the verification plan from Audit as a **blackbox** — the agent
running Verify does not need to understand the implementation. It:

1. Runs every automated test listed in the plan
2. Runs every smoke test command and captures output
3. Produces a structured comparison report
4. Updates `provider-migration-recipe.md` with discoveries
5. Final `GRAFANACTL_AGENT_MODE=false make all`

**Gate**: User reviews comparison report. All discrepancies must be justified
or fixed.

### Why 3 stages, not 4

Migrations don't need a separate Design stage. Unlike greenfield providers
(where `/add-provider` needs Discover → Design to research APIs and make
auth/client/envelope decisions), migrations inherit these decisions from the cloud CLI.
The architectural mapping in Audit handles the translation — it's a mapping
exercise, not a design exercise. A 4th "Design" stage would become a
rubber-stamp gate that teaches agents to skip gates.

### Why dual blackbox isolation

This is **stage-level TDD**. In traditional TDD, you write the test before
the implementation to prevent the test from being shaped by implementation
knowledge. Here, the same principle applies at the workflow level:

1. **Build is blackbox from Verify**: The builder doesn't know what smoke
   tests will be run, so it can't overfit to them. It implements to the
   *requirements* (parity table + arch mapping), writes unit tests based on
   those requirements, and trusts that correct implementation will pass
   verification.

2. **Verify is blackbox from Build**: The verifier doesn't know
   implementation details, so it checks *behavior* not *structure*. It runs
   pre-defined commands and compares outputs — confirmation bias is
   impossible because the verifier has no beliefs about how the code works.

This also enables Build and Verify stages to run in different sessions or by
different agents without loss of information. Each stage is self-contained
with its sealed envelope from Audit.

### Rejected Alternatives

**A: 4-Stage Mirror of /add-provider** — Structural consistency is nice but
a mostly-empty Design stage is worse than no Design stage. It trains agents
that gates are optional.

**B: Spec Pipeline Delegation** — Spec infrastructure (`/plan-spec` +
`/build-spec`) adds overhead for mechanical ports. Generating spec.md +
plan.md + tasks.md for a recipe-following task is ceremony without value.
The spec pipeline is designed for design-heavy features, not translation work.

## Consequences

### Positive

- Verification plan exists before implementation → catches specification
  gaps early (e.g., "we forgot to plan for the `activity` subcommand")
- Dual blackbox prevents both overfitting (Build can't see tests) and
  confirmation bias (Verify can't see implementation)
- Stage-level TDD: spec and tests written together, implementation and
  checking run independently — same discipline as function-level TDD but
  applied to the entire migration workflow
- 3 stages with real gates (vs 5 phases with implicit ordering) is easier
  for agents to follow without skipping
- Architectural mapping forces explicit translation of cloud CLI patterns →
  prevents copy-paste of incompatible patterns
- Each stage is self-contained via sealed envelopes → enables session
  boundaries, agent handoffs, and resume without context reconstruction

### Negative

- Structural divergence from `/add-provider` — agents can't pattern-match
  across skills. Mitigated by shared conventions (gate format, orchestration
  tables, checklist format)
- Build stage lumps 3+ recipe phases into one stage — risk of recreating
  the flat-checklist problem. Mitigated by `make lint` checkpoints between
  phases and the architectural mapping constraining each phase's approach
- Sealed envelope enforcement is honor-system in practice — a single-session
  agent has all context available. Mitigated by skill instructions explicitly
  stating which artifacts each stage receives, and by structuring file output
  so envelopes are in separate files

### Follow-up

- ~~Update `provider-migration-recipe.md` to reference the new skill structure~~ Done in this PR.
- First validation: next provider port from epic grafanactl-experiments-0zr
- Build-Core / Build-Commands split adopted in the initial design (see Stage 2 in SKILL.md); monitor whether the TaskList coordination pattern holds up for providers with 3+ resource types.
