---
name: migrate-provider
description: >-
  Reference for porting a Grafana Cloud product from the legacy
  grafana-cloud-cli into a gcx provider — adapter, schema/example registration,
  CRUD redirect commands. HUMAN-DRIVEN — this skill is outside the
  integrate-with-gcx v1 autonomous workflow and must not be run end to end
  unattended because its non-registration steps have not been fully audited.
  Use when a human is deliberately driving a port and says "migrate provider",
  "port from gcx", "port oncall", "port k6". Not for building providers from
  scratch — use /add-provider. Recheck present-day placement before porting.
---

# Migrate a Provider from the Legacy CLI

Port an existing `grafana-cloud-cli` resource client into this repository's gcx
provider system — core adapter, schema/example registration, CRUD redirect
commands, and ancillary subcommands.

> **Read this before the recipe. A human drives this skill; it is not covered by
> the integrate-with-gcx v1 workflow.** In these instructions, "legacy CLI"
> means `grafana-cloud-cli`, whose binary was also named `gcx`; "gcx" means this
> repository's `bin/gcx`. Use `bin/gcx` for the build under review, and name the
> legacy binary's own path explicitly.
>
> Of the recipe's mechanical steps, only the registration flow
> (`providers.Register()` + `TypedRegistrations()`, and the `TypedCRUD`
> `Descriptor`/`Aliases` fields) has been checked against current code. Treat the
> rest as unaudited and verify as you go.

**Before starting:** read `gcx-provider-recipe.md` front to back for the
mechanical steps, subject to the caveat above. This skill wraps it with workflow
discipline and orchestration.

**Canonical reference:** the `incidents_*.go` files in
`internal/providers/irm/` — the first full port
(adapter + schema + commands + ancillary). Start there for patterns.

## When to Use

- Porting a legacy `grafana-cloud-cli` resource client into current gcx
- A bead task references legacy provider migration
- User says "migrate provider", "port from gcx", "port oncall", "port k6"

**When NOT to use**: Building a provider from scratch for a product without
a legacy CLI client — use `/add-provider` instead.

## Relationship to integrate-with-gcx

One-directional, so the two skills cannot bounce a port back and forth:

- **Recheck placement before porting.** A legacy client proves that an API
  existed; it does not prove that the provider tier is still the right home.
  Check current `/apis` CRUD coverage and inventory the non-CRUD operations. A
  human confirms provider-tier placement before Phases 0-4 continue.
- **`integrate-with-gcx` does not route work here**, because of the status note
  above: it tells the user a port needs a human to drive and stops. Reach this
  skill by invoking it deliberately.
- **Call back into two sections only**, not the whole skill: the naming pass on
  the ported command surface (`self-review.md` T7 — released names are frozen,
  and a port is where a legacy name most often gets carried in), and the
  diff-triggered review before requesting human review (`self-review.md`).
  Contract worksheets and the general readiness workflow do not apply to a
  port; the placement recheck above is still required.
- Read from the checkout at
  `.claude/skills/integrate-with-gcx/references/self-review.md`, which carries
  T7 and the trigger table.

## Prerequisites

Before invoking this skill, ensure:

1. **Current gcx binary built** — `bin/gcx --version` must succeed.
2. **Grafana context configured** — `bin/gcx config view` must show a working
   context with server URL and token.
3. **Provider directory exists** — create `internal/providers/{name}` before
   starting the port.
4. **Live API access** — smoke tests (Phase 4) require a real Grafana instance.
   Verify connectivity: `bin/gcx --context=<ctx> resources list-types`.

## Pipeline Overview

```
Phase 0: Requirements Gathering (autonomous)
  → context bundle (legacy source + compliance + pattern ref)
      ↓ [no gate — feeds Phase 1]
Phase 1: Design Discovery (interactive, 1A–1D)
  → ADR in docs/adrs/{provider}/
      ↓ [user approval gate]
Phase 2: Spec Planning
  → spec.md + plan.md + tasks.md
      ↓ [user approval gate]
Phase 3: Build
  → agent team (Core + Commands)
  → code files
      ↓ [GCX_AGENT_MODE=false mise run all gate]
Phase 4: Verification (4A–4E)
  → GCX_AGENT_MODE=false mise run all + smoke tests + adapter smoke
  → comparison report + recipe update
      ↓ [user approval gate]
```

| Phase | Agent Strategy | Receives | Produces | Gate |
|-------|---------------|----------|----------|------|
| 0: Requirements | Lead (autonomous) | legacy source + compliance docs | Context bundle | None (feeds Phase 1) |
| 1: Design | Lead (interactive) | Context bundle | ADR | User approves ADR |
| 2: Spec Planning | Lead (or `/plan-spec`) | ADR + context bundle | spec.md, plan.md, tasks.md | User approves spec package |
| 3: Build | Agent team (Core + Commands) or `/build-spec` | Spec package | Provider code | `GCX_AGENT_MODE=false mise run all` passes |
| 4: Verify | Subagent | Comparison report template + spec ACs | Comparison report + recipe update | User approves report |

Phases are **strictly sequential**. Each phase is separated by a gate that
**must pass** before the next phase begins. Gates are not optional.

> **Small-provider shortcut:** For providers with 3 or fewer subcommands,
> Phase 1 stages 1B–1D may be collapsed into a single proposal. Document
> this choice in the ADR.

---

## Phase 0: Requirements Gathering (Autonomous)

Phase 0 is fully autonomous — no user interaction required. The output is a
context bundle, not a design proposal.

### 0.1: Read the Legacy CLI Source

Read the grafana-cloud-cli source for the target provider. Identify every
subcommand, API endpoint, type definition, and auth mechanism.

### 0.2: Check Compliance Documents

Read the following project compliance documents and record which rules apply
to the target provider. Use the lint compliance checklist from `conventions.md`
as the recording template.

- `CONSTITUTION.md` — CLI grammar, output conventions
- `docs/design/naming.md` — resource, file, config, and flag naming conventions
- `docs/design/command-naming.md` — canonical command verbs and placement
- `docs/design/output.md` — output formats
- `docs/design/exit-codes.md` — exit codes
- `docs/reference/provider-guide.md` — provider interface, adapter wiring
- `docs/reference/provider-discovery-guide.md` — API discovery, design decisions

### 0.3: Identify Pattern Reference

Identify and read the closest existing gcx provider as a pattern reference:
- **Cloud APIs with separate URLs** → `k6` (fleet no longer applies: it uses the plugin proxy)
- **Plugin APIs (standard Grafana SA token)** → `slo`
- **gRPC-style POST APIs behind a plugin proxy** → `fleet`
- **gRPC-style POST APIs** → `incidents`
- **Token exchange auth** → `k6`
- **Multi-resource providers** → `oncall`
- **Plugin proxy APIs** → `kg`

### 0.4: Produce Context Bundle

The context bundle contains:
1. **Source summary** — every legacy CLI subcommand mapped to its proposed gcx equivalent or "Deferred" with rationale
2. **Compliance notes** — applicable rules per document with section references (filled checklist from 0.2)
3. **Pattern reference** — which existing provider to follow and why

### Phase 0 Gate

> None. Phase 0 feeds directly into Phase 1. The context bundle is an
> internal artifact — it does not require user approval.

---

## Phase 1: Design Discovery (Interactive)

Phase 1 uses progressive disclosure with four stages. Each stage MUST receive
explicit user approval before the next stage begins.

### Stage 1A: CLI UX

Propose a command tree with naming and grammar compliance validated against
CONSTITUTION.md's CLI Grammar section.

Present to user: command tree, verb choices, alias conventions, naming rationale.

**Gate:** User approves Stage 1A before proceeding.

### Stage 1B: Resource Adapters

Specify which resources get TypedCRUD adapters, which remain provider-only
commands, the GVK mapping for each adapter resource, and the verb choice
rationale (list vs show).

Present to user: adapter classification table, GVK mapping, verb rationale.

**Gate:** User approves Stage 1B before proceeding.

### Stage 1C: Auth & Config

Specify ConfigKeys, ConfigLoader usage, environment variable names, and any
GCOM/instance lookup requirements.

Present to user: config key table, env var names, auth flow diagram.

**Gate:** User approves Stage 1C before proceeding.

### Stage 1D: Architecture

Specify package layout, client construction pattern, shared helpers, and the
auth subpackage structure (if the provider uses multiple subpackages).

Present to user: package tree, client pattern, helper inventory.

**Gate:** User approves Stage 1D before proceeding.

> **Small-provider shortcut:** For providers with 3 or fewer subcommands,
> collapse stages 1B–1D into a single combined proposal. All content must
> still be present — only the number of approval rounds is reduced.

### Phase 1 Output: ADR

Write an ADR documenting all design decisions from stages 1A–1D to
`docs/adrs/{provider}/`. The ADR MUST be approved by the user before
proceeding to Phase 2.

### Phase 1 Gate

> **STOP.** Do not begin Phase 2 until:
>
> 1. All four stages (1A–1D) have received explicit user approval
> 2. The ADR exists in `docs/adrs/{provider}/` and is approved by the user
>
> If the user has NOT approved a stage, block and re-present it for feedback.

---

## Phase 2: Spec Planning

Phase 2 produces three documents in spec format:

1. **spec.md** — functional requirements + acceptance criteria (Given/When/Then)
2. **plan.md** — architecture decisions + HTTP client reference section
3. **tasks.md** — dependency graph + waves + per-task deliverables

### plan.md: HTTP Client Reference (MANDATORY)

plan.md MUST include the HTTP client reference section from
`commands-reference.md`. This section contains:
- Endpoint table (method, path, purpose, notes)
- Auth helper signature
- Client construction pattern with exact field names

This prevents response envelope hallucination — the most impactful bug class
discovered during provider migrations.

### tasks.md: Verification Tasks (MANDATORY)

tasks.md MUST include smoke test design as explicit verification tasks. Each
show/list command MUST have a smoke test task entry specifying all four output
formats (json, table, wide, yaml).

### Optional: /plan-spec Integration

When `/plan-spec` is available, Phase 2 SHOULD use it. When `/plan-spec` is
not available, Phase 2 MUST produce the same document format manually.
`/plan-spec` is an optional accelerator, not a dependency.

### Phase 2 Gate

> **STOP.** Do not begin Phase 3 until:
>
> 1. All three documents (spec.md, plan.md, tasks.md) exist with YAML
>    frontmatter, FR-NNN numbering, and Given/When/Then acceptance criteria
> 2. plan.md contains the HTTP client reference section
> 3. tasks.md contains smoke test verification tasks for all output formats
> 4. The user has explicitly approved the spec package

---

## Phase 3: Build

Phase 3 executes tasks.md waves in order, with `mise run lint` as a checkpoint
between each task.

### Builder Agent Rules

Builder spawn prompts (see `templates/builder-prompts.md`) MUST include:

> "Do NOT infer response envelope shapes. Copy deserialization code verbatim
> from the grafana-cloud-cli source. If the source does
> `json.Unmarshal(body, &slice)`, the new client MUST do the same — never
> wrap in a struct unless the source does."

Builder spawn prompts MUST NOT include verification task details — no smoke
commands, no expected comparison outputs, no pass/fail criteria from the
comparison report template.

### Agent Team Orchestration

The Build phase uses an agent team with two teammates:
- **Build-Core** — owns types, client, adapter, resource_adapter files.
  Must complete before Build-Commands begins.
- **Build-Commands** — owns provider registration and CLI command files.
  Starts only after Build-Core signals completion.

### File Ownership Table

| Recipe Phase | File(s) | Teammate |
|---|---|---|
| Step 2: Types | `internal/providers/{name}/types.go` | Build-Core |
| Step 3: Client | `internal/providers/{name}/client.go`, `client_test.go` | Build-Core |
| Step 4: Adapter + Resource Adapter | `internal/providers/{name}/adapter.go`, `resource_adapter.go` | Build-Core |
| Step 5: Provider registration | `internal/providers/{name}/provider.go` | Build-Commands |
| Step 6: Tests | Command tests (`*_test.go`) | Build-Commands |
| Step 7: Integration / Wiring | `cmd/gcx/providers/{name}/commands.go`, `cmd/gcx/root/command.go` (blank import) | Build-Commands |

Teammates MUST NOT modify files outside their ownership boundary.

### Integration/Wiring Task

The integration task MUST explicitly include:
1. Wire `Commands()` and `TypedRegistrations()`
2. Add blank import in `cmd/gcx/root/command.go`
3. Fix import cycles introduced by subpackage references
4. Fix variable name collisions from package aliasing
5. Run `mise run lint` and fix all new issues

### Optional: /build-spec Integration

When `/build-spec` is available, Phase 3 SHOULD use it. When `/build-spec` is
not available, Phase 3 MUST use the agent team orchestration described above.
`/build-spec` is an optional accelerator, not a dependency.

### Phase 3 Gate

> **STOP.** Do not begin Phase 4 until:
>
> `GCX_AGENT_MODE=false mise run all` exits 0 with no lint errors and all tests
> passing.
>
> Run this command after both Build teammates complete. If it fails, fix the
> root cause before proceeding — do not proceed with a failing build.

---

## Phase 4: Verification (4A–4E)

Phase 4 MUST execute in this exact order. No step may be skipped.

### Step 4A: Build Gate

Run `GCX_AGENT_MODE=false mise run all` and confirm exit 0.

### Step 4B: Smoke Tests (MANDATORY)

Smoke tests are MANDATORY for every show/list command. Each command MUST be
tested with ALL FOUR output formats: `-o json`, `-o table`, `-o wide`,
`-o yaml`.

Smoke tests MUST NOT be silently skipped or quietly downgraded.
If no live instance is available, report every smoke test as UNVERIFIED with that
reason and do NOT assert parity with the legacy CLI — an unverified port is an
honest state, a claimed-but-untested one is not. Report the blocker
to the user.

```bash
CTX={context-name}

for fmt in json table wide yaml; do
  GCX_AGENT_MODE=false bin/gcx --context=$CTX {resource} list -o $fmt > /dev/null 2>&1 \
    && echo "list $fmt: OK" || echo "list $fmt: FAIL"
done
```

### Step 4C: Adapter Smoke (MANDATORY)

Every TypedCRUD resource MUST be verified via the adapter path:
- `resources list-types` — registration visible
- `resources get {alias}` — envelope + deserialization working

### Step 4D: Spec Compliance

Check every acceptance criterion from spec.md. Report SATISFIED or UNSATISFIED
with file:line evidence.

### Step 4E: Recipe Update (MANDATORY)

Update `gcx-provider-recipe.md` with:
1. **Status tracker entry** — a new row for the ported provider
2. **Gotchas section** — problems discovered during smoke tests (or explicit
   "No new gotchas" if none)
3. **Pattern corrections** — if any recipe step was unclear or incorrect

### Comparison Report

Produce a structured comparison report using `templates/comparison-report.md`.
Present it to the user for review.

### Phase 4 Gate

> **STOP.** Do not declare the migration complete until:
>
> 1. The comparison report has been produced and presented to the user
> 2. Every discrepancy is either justified with written rationale or fixed
> 3. The recipe update (Step 4E) is complete
> 4. The user has explicitly approved the comparison report

---

## Red Flags — STOP and Check

When you notice any of these during execution, stop and take the corrective
action before continuing.

| Red Flag | Rationalization | STOP. Do this instead |
|---|---|---|
| **Inferring response envelope shapes** instead of copying from the legacy CLI source | "The response shape is obvious from the type definition" | Copy deserialization code verbatim from the legacy CLI. If it does `json.Unmarshal(body, &slice)`, do the same. Never wrap in a struct unless the source does. |
| **Copying the legacy client verbatim** — embedding `*grafana.Client`, using `c.Get()`/`c.Post()` | "The legacy client already works, adapting it would just introduce bugs" | Translate to gcx's typed HTTP client pattern. Read recipe Step 3 and the current provider guide. |
| **Skipping the source audit** — jumping to implementation | "I can see the important commands, a full audit is redundant" | Phase 0 is required. Every legacy CLI subcommand must appear in the source summary. |
| **Guessing endpoint names or paths** | "The endpoint pattern is obvious from the resource name" | Read the legacy CLI source for exact paths. Never guess. |
| **Skipping smoke tests** — marking Phase 4 complete without running commands | "The unit tests pass, so the implementation is correct" | Smoke tests are mandatory. Block and tell the user if no live instance is available. |
| **Builder reading verification tasks** — checking smoke commands during Phase 3 | "I need to check what smoke tests will run to make sure my code will pass" | Builders receive spec + plan + implementation tasks. Not verification tasks. |
| **Build-Commands starting before Build-Core completes** | "I can start on the command structure while Core finishes types" | Wait for Build-Core to complete. Commands depend on adapter interfaces. |
| **Skipping a phase gate** | "The previous phase was straightforward, I can proceed" | Every gate must be passed. No exceptions. |
| **Producing custom artifact formats** instead of spec documents | "A parity table is simpler than a full spec" | Use spec document format (spec.md, plan.md, tasks.md). No custom artifacts. |
