---
name: add-provider
description: Use for the implementation workflow once a capability is already classified as a Grafana Cloud product provider (SLO, OnCall, Synthetic Monitoring, k6, ML, etc.) — provider package, commands, config keys, optional resource adapters. Trigger on "add provider", "new provider", "build the [product] provider". NOT for deciding whether something should be a provider at all, for integration contracts, or for pre-review self-checks — use the integrate-with-gcx skill for those (bundled; `gcx agent skills get integrate-with-gcx`).
---

# Add Provider

Orchestrates adding a new Grafana product provider — from API discovery through
verified implementation. Four stages, worked autonomously: the stage boundaries
are checkpoints you satisfy, not approvals you wait for.

## When to Use

- User wants to add CLI support for a Grafana Cloud product
- User says "add provider", "new provider", "integrate [product]"
- A bead task references provider implementation

**When NOT to use**: if all you need is standard CRUD on a type that is
externally accessible and discoverable on `/apis` — `gcx resources` already covers
that through dynamic discovery.

That test is about CRUD, not about the whole command surface. `gcx dashboards` and
`gcx alert` are dedicated command trees over products that *are* on `/apis`,
because their real operations (restore a version, export a policy tree) are not
CRUD verbs. So a K8s-backed product can still warrant commands; run the placement
analysis rather than stopping at "it's on `/apis`".

If the answer is a commands-only provider calling the K8s dynamic client, note
that `CONSTITUTION.md` § Provider Architecture makes
`internal/providers/dashboards/` the one documented exception (ADR 016) — a second
requires explicit human approval and a CONSTITUTION change.

## Entry paths

**Invoked from `integrate-with-gcx`** (the placement section already exists —
necessity, command path, backend evidence, wiring, readiness). Then:

- Skip Stage 1 entirely if the placement section carries the API surface, auth
  model and readiness verdict. Record those findings and move on; do not
  re-research or re-classify what is already settled, and do not ask for
  approval of decisions that were made with evidence.
- Skip the Stage 2 decisions it already answers (tier, command surface, and
  whether the resource belongs in the `resources` pipeline). Answer only what is
  genuinely still open.
- The Stage 1 and Stage 2 **blocking approvals** do not apply on this path: you
  do not wait for sign-off before implementing. Build autonomously. If something
  is genuinely unsettled, discover it, or ask one targeted question carrying the
  evidence and a recommendation — never fall back to a blanket approval gate.
- **Write documents only where they earn their keep.** The Stage 2 artifacts are
  conditional on real architectural risk or a repository requirement, not
  mandatory paperwork:
  - **ADR** — only for a decision that is contested, hard to reverse, or departs
    from precedent: a new auth model, a client type the repo has not used, an
    adapter registration, a cross-cutting config change. "Reuse the stack token,
    hand-rolled HTTP client, commands-only" is the documented default across
    existing providers — record it in the PR body and move on.
  - **Spec / per-stage documents** — only when the work is genuinely staged
    across multiple PRs, which is what they exist to make resumable. A provider
    shipping in one change does not need them.
  - **Smoke-test plan** — always, because Stage 4 executes it and the repo
    requires real-instance verification. Keep it to the commands you actually
    implemented.
- Start at Stage 3, and use Stage 4 verification as written.

**Invoked directly** (no placement section): work through all four stages, and
check `references/decision-tree.md` first to confirm a provider is the right
approach. Autonomy is the same as above — the stage gates are **checkpoints you
satisfy, not approvals you wait for**:

- Discover and decide from the repository, the product's API docs and a probe.
  Present the research and design findings as you go; do not stop for sign-off.
- Ask only where an unresolved answer would materially change the implementation —
  a missing auth model, an API shape you cannot verify, a frozen command name the
  naming guide and precedent record do not settle. Group those questions, carry
  the evidence and a recommendation.
- Documents follow the same risk test as above: ADR for contested or
  precedent-departing decisions, spec only for genuinely staged work,
  smoke-test plan always.
- Stop only for a CONSTITUTION conflict with no compliant alternative, a needed
  waiver, or a missing backend/auth prerequisite.


## The flow, however you got here

```text
contract (proportional)  →  implementation  →  Review
```

Both entry paths run all three, and none of them is a document or a gate:

- **Contract, before code** — `claude-plugin/skills/integrate-with-gcx/references/contract-and-tests.md`,
  sized to the change. If you arrived from `integrate-with-gcx` the contract
  already exists; use it, don't redo it.
- **Review, before calling it review-ready** —
  `claude-plugin/skills/integrate-with-gcx/references/self-review.md`, re-run after
  every fix push.

That is where the naming, typed-input, output-class, completeness, error,
token-cost and test-quality guidance lives. Read those two rather than restating
them here.

## Workflow

```
Discover ───────> Design ───────> Implement ──gate──> Verify
   │                  │                  │                  │
   v                  v                  v                  v
research report    ADRs + spec       code per stage     smoke tests
```

| Stage | Deliverable | Gate |
|-------|-------------|------|
| 1. Discover | research findings (report only if staged work needs one) | findings presented; no approval wait |
| 2. Design | decisions + smoke test plan (ADR/spec only per the risk test) | decisions presented; no approval wait |
| 3. Implement | Code (one stage at a time) | `mise run gate` passes per stage |
| 4. Verify | Smoke tests + architecture doc updates | smoke tests run or reported UNVERIFIED; wiring checks pass |

### Prerequisites

Know these before starting — from the placement section on the
`integrate-with-gcx` path, or by asking on direct invocation:
- **Product name** — which Grafana product to integrate
- **Access** — is there a running Grafana instance with the product enabled?
- **Scope** — full provider or single resource type first?

---

## Stage 1: Discover

> **Guide**: `docs/reference/provider-discovery-guide.md` Sections 1.1–1.6

### 1a. Establish context yourself

Research these before asking anything — the repo, the module cache, the vendor's
docs and a probe answer most of them:

1. Source code or OpenAPI spec for the product's API
2. Terraform provider coverage, if any, as a schema reference
3. An existing Go client library
4. Quirks — non-standard auth, async operations, unusual pagination

Ask only where the answer is both unavailable and load-bearing, in one grouped
question with the evidence and a recommendation. Do not open with a
questionnaire.

### 1b. Research

Follow `provider-discovery-guide.md` Sections 1.1–1.6:
- Map API surface (base path, auth, endpoints, pagination)
- Check existing tooling (Terraform schemas, Go SDK)
- Inspect source code (undocumented endpoints, enum values)
- Identify auth model
- Map resource relationships
- Test API behavior with real calls

### 1c. Write Research Report

Write findings to `docs/research/YYYY-MM-DD-{product}-provider.md` using
the template at `docs/_templates/research.md`. Must include:

- API endpoints and response shapes discovered
- Auth model analysis
- Resource relationships
- At least one successful probe result — or, if no instance is reachable, the
  probe you would run, marked UNVERIFIED with the reason
- Confidence assessment per finding

### Checkpoint: Research Complete

Present the findings and keep going — this is a checkpoint, not an approval
wait, on either entry path. Ask only if something unresolved would materially
change the implementation. On the `integrate-with-gcx` path the stage is skipped
outright when the placement section already answers it — see
[Entry paths](#entry-paths).

---

## Stage 2: Design

> **Guide**: `docs/reference/provider-discovery-guide.md` Section 2

### 2a. Design Decisions

Answer each decision from the guide, grounded in research findings:

1. **Auth strategy** — reuse Grafana token or separate credentials?
2. **Client type** — plugin API, K8s API, or external service?
3. **Adapter-backed or commands-only?** — plain provider commands are valid on
   their own; register a `ResourceAdapter` (via `TypedRegistrations()`) only when
   the resource genuinely belongs in the `gcx resources` push/pull pipeline. Never
   create an adapter merely to unlock a CRUD verb (CONSTITUTION § Provider
   Architecture).
4. **Envelope mapping** *(adapter-backed resources only)* — how do API objects map to the K8s envelope?
5. **Command surface** — which verbs are actually implemented (CRUD subset + beyond-CRUD)?
6. **Package layout** — flat or subpackaged?
7. **Staging** — how to break into shippable stages?

For beyond-CRUD commands: brainstorm based on real APIs found in research
(status, timeline, validation, etc.). Present options to user — include
"CRUD only for now" as an option.

### 2b. Write ADRs

Write an ADR in `docs/adrs/{product}-provider/NNN-{decision}.md` (template:
`docs/_templates/adr.md`) for each decision that is **contested, hard to reverse,
or departs from precedent** — typically a new auth model, a client type the repo
has not used before, or an adapter registration.

A decision that matches the documented default across existing providers does not
need an ADR: record it in the PR body. "Reuse the Grafana token, hand-rolled HTTP
client, commands-only" is the default, not a novel choice. Writing an ADR to
restate it is paperwork, and reviewers have to read it.

Other decisions can be captured in the spec if they're straightforward.

### 2c. Write Spec

Write the implementation plan in `docs/specs/{product}-provider/` **when the work
is genuinely staged across multiple PRs** — that is what per-stage documents exist
for, making the work resumable in a fresh session. A provider that ships in one
change does not need them; put the plan in the PR description.

When staging is real:

- Top-level plan with all stages, file tree, and decisions summary
- Per-stage docs with scope, files to create, and acceptance criteria

Use the templates in `docs/_templates/` for structure; `docs/plans/` holds
precedent planning documents.

### 2d. Write Smoke Test Plan

**Every stage doc MUST include a Verification section** with concrete smoke
test commands using real values (not placeholders). These are executed in
Stage 4 after implementation.

Cover only the verbs the stage actually implements — do not smoke-test CRUD
verbs the provider doesn't expose. Destructive commands use `--force`
(never `--yes`; see `docs/design/safety.md` §3.2). Example pattern (replace
with real product/resource names in actual spec):
```bash
# Provider appears in list
bin/gcx providers list | grep {name}

# Config secrets are redacted
bin/gcx config view | grep {name}

# Implemented operations work (subset per stage)
bin/gcx {name} {resource} list
bin/gcx {name} {resource} get <test-id>
bin/gcx {name} {resource} delete <test-id> --force

# Unified resources path works (adapter-backed resources only)
bin/gcx resources get {alias}
```

### Checkpoint: Design Complete

Present the decisions — and whichever of ADR/spec the risk test actually called
for — and keep going. No approval wait on either entry path. On the
`integrate-with-gcx` path, decisions the placement section already settled are
recorded rather than re-derived — see [Entry paths](#entry-paths).

---

## Stage 3: Implement

> **Guide**: `docs/reference/provider-guide.md` (Steps 1–7)
> **UX Guide**: `docs/design/`

Implement one stage at a time per the plan. Each stage's doc is
self-contained enough to resume in a fresh session.

If `/build-spec` or `/build-task` skills are available, use them to drive
implementation. Otherwise, follow `provider-guide.md` Steps 1–7 directly.
Summary of the key steps:

1. Provider interface + `init()` with a single `providers.Register()` call + `providers.ConfigLoader` (mirror the SLO reference)
2. Config keys + validation
3. Commands with UX compliance
4. Types + client; adapter only for resources placed in the `resources` pipeline (returned from `TypedRegistrations()`, non-nil `Schema`)
5. Register (blank import in `cmd/gcx/root/command.go`; adapter registration flows through `TypedRegistrations()` — never call `adapter.Register()` directly)
6. Tests (interface compliance, client httptest request mapping; adapter round-trip only when an adapter exists)

**Key patterns** (see provider-guide.md for details):
- Hand-roll HTTP client (~200 LOC) — don't use generated OpenAPI clients
- Use `providers.ConfigLoader` (instantiate once in `Commands()`, `BindFlags` on the parent) — don't hand-roll config loading or import `cmd/gcx/config`
- Config key names use hyphen-case
- Adapter-backed resources must strip server-generated fields on Create/Update

### Gate: Stage Complete

Per stage: `mise run gate` (lint + tests + build) passes, no regressions. Run the
full `GCX_AGENT_MODE=false mise run all` **once** before pushing — it adds
validate-skills and the docs build on top of the same lint/tests/build, so
repeating it per stage buys nothing and costs minutes each time.

---

## Stage 4: Verify

### 4a. Run Smoke Tests

Execute every smoke test command from the Stage 2d verification plan against a
real Grafana instance, using `bin/gcx` so you exercise the build under review.
Record results (pass/fail + output).

If no instance or credentials are available, report every smoke test as
**UNVERIFIED** with that reason, and say what a reviewer must run before merge.
Do not block on it, and do not report untested commands as passing — an
`httptest`-proven client with UNVERIFIED smoke tests is an honest state; a silent
gap is not.

### 4b. Run Checklists

From `docs/design/provider-checklist.md` and `docs/reference/provider-guide.md`:

**Interface**: All 6 Provider methods (incl. `TypedRegistrations()` — `nil` is
valid for commands-only providers), `Name()` lowercase/unique, ConfigKeys
complete, secrets marked, Validate returns actionable errors, blank import added.

**UX**: `-o json/yaml` support, text table default, actionable error suggestions,
no `os.Exit()`, cmdio status messages, help text standards, push idempotent,
format-agnostic data fetching, promql-builder for PromQL.

**Agent contract**: every new leaf command has an entry in
`cmd/gcx/root/testdata/output_classes.json` and a token-cost annotation (plus
`llm_hint` whenever the worst case is medium/large) — the `TestConsistency_*` and
`TestAgentConformance_*` suites in `cmd/gcx/root/` fail CI on a missing output
class or token cost. The hint check is weaker: it matches the annotation exactly
against `"medium"`/`"large"`, so a qualified cost like `small (large with --all)`
evades it. Write the hint regardless — the rule is the worst case, not the
spelling.

**Build**: `GCX_AGENT_MODE=false mise run all` once, then `bin/gcx providers list` lists it and `bin/gcx config view` redacts its secrets.

### 4c. Update Architecture Docs

Follow `docs/reference/doc-maintenance.md` structural checks — a new provider
adds packages to `internal/` and commands to `cmd/`, so architecture docs
need updating.

### Checkpoint: Verified

Smoke tests executed and recorded, or reported UNVERIFIED with the reason and
what a reviewer must run before merge. Wiring checks pass; docs updated.

---

## Reference Implementations

| Provider | Auth Model | API Type | Key Entry Point |
|----------|-----------|----------|-----------------|
| SLO | Same Grafana token | Plugin API | `internal/providers/slo/provider.go` |
| Synth | Separate URL + token | External service | `internal/providers/synth/provider.go` |

## Common Pitfalls

| Pitfall | Mitigation |
|---------|------------|
| K8s CRDs not externally accessible | Verify with real API call before choosing K8s client |
| Incomplete OpenAPI specs | Cross-reference with source code route handlers |
| Hand-rolled config loading | Use `providers.ConfigLoader` (see SLO reference); never import `cmd/gcx/config` from `internal/providers/` |
| Missing blank import | Add `_ ".../{name}"` in `cmd/gcx/root/command.go` |
| Adapter created just for CRUD verbs | Commands-only providers are first-class; adapters only for resources that belong in the `resources` pipeline |
| readOnly fields in POST/PUT | Adapter must strip server-generated fields |
| Missing output class / token cost | New leaves fail `TestConsistency_*` in `cmd/gcx/root/` — add the `output_classes.json` entry + annotation |
