---
name: add-provider
description: Use for the implementation workflow once a capability is already classified as a Grafana Cloud product provider (SLO, OnCall, Synthetic Monitoring, k6, ML, etc.) — provider package, commands, config keys, optional resource adapters. Trigger on "add provider", "new provider", "build the [product] provider". NOT for deciding whether something should be a provider at all, for integration contracts, or for pre-review self-checks — use the integrate-with-gcx skill for those (bundled; `gcx agent skills get integrate-with-gcx`).
---

# Add Provider

Orchestrates adding a new Grafana product provider — from API discovery through
verified implementation. Four stages; the discovery and design gates apply to
direct invocation only (see [Entry paths](#entry-paths)).

## When to Use

- User wants to add CLI support for a Grafana Cloud product
- User says "add provider", "new provider", "integrate [product]"
- A bead task references provider implementation

**When NOT to use**: If the product exposes a K8s-compatible `/apis` endpoint,
it already works with `gcx resources` — no provider needed.

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
- **Still produce the Stage 2 deliverables** — the ADRs, the spec, and the
  smoke-test plan. A four-bullet placement section settles the tier, the path,
  the backend and readiness; it does not contain an auth-strategy ADR or a
  per-stage verification plan, so those are written here, not skipped. What
  changed is that they are reviewed with the PR instead of gating it.
- Start at Stage 3, and use Stage 4 verification as written.

**Invoked directly** (no placement section): run all four stages below,
including their gates, and check `references/decision-tree.md` first to confirm a
provider is the right approach.

## Workflow

```
Discover ──gate──> Design ──gate──> Implement ──gate──> Verify
   │                  │                  │                  │
   v                  v                  v                  v
research report    ADRs + spec       code per stage     smoke tests
```

| Stage | Deliverable | Gate |
|-------|-------------|------|
| 1. Discover | `docs/research/` report | User approves findings *(direct invocation only)* |
| 2. Design | ADRs + spec + smoke test plan | User approves design *(direct invocation only)* |
| 3. Implement | Code (one stage at a time) | `mise run all` passes per stage |
| 4. Verify | Smoke tests + architecture doc updates | All checks green |

### Prerequisites

Know these before starting — from the placement section on the
`integrate-with-gcx` path, or by asking on direct invocation:
- **Product name** — which Grafana product to integrate
- **Access** — is there a running Grafana instance with the product enabled?
- **Scope** — full provider or single resource type first?

---

## Stage 1: Discover

> **Guide**: `docs/reference/provider-discovery-guide.md` Sections 1.1–1.6

### 1a. Gather User Context

Before autonomous research, ask what the user already knows:

1. Source code access — which repo?
2. API documentation — OpenAPI specs, Grafana docs URLs?
3. Terraform resources — does the Terraform provider support this product?
4. Go SDK — existing Go client library?
5. Known quirks — non-standard auth, async ops, unusual pagination?

Use answers to skip known areas and focus research on gaps.

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
- At least one successful API call result
- Confidence assessment per finding

### Gate: User Approves Research

Direct-invocation path only. Present the research report; do not proceed to
design until approved. On the `integrate-with-gcx` path this stage and its gate
are skipped — see [Entry paths](#entry-paths).

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

For each significant decision, write an ADR in
`docs/adrs/{product}-provider/NNN-{decision}.md` using the template at
`docs/_templates/adr.md`. At minimum, create ADRs for:

- Auth strategy choice
- Client type choice (plugin API vs K8s vs external)

Other decisions can be captured in the spec if they're straightforward.

### 2c. Write Spec

Write the implementation plan in `docs/specs/{product}-provider/`:

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
gcx providers list | grep {name}

# Config secrets are redacted
gcx config view | grep {name}

# Implemented operations work (subset per stage)
gcx {name} {resource} list
gcx {name} {resource} get <test-id>
gcx {name} {resource} delete <test-id> --force

# Unified resources path works (adapter-backed resources only)
gcx resources get {alias}
```

### Gate: User Approves Design

Direct-invocation path only. Present ADRs and spec; do not proceed to
implementation until approved. On the `integrate-with-gcx` path, decisions the
placement section already settled are recorded rather than re-derived, and there
is no approval gate — see [Entry paths](#entry-paths).

---

## Stage 3: Implement

> **Guide**: `docs/reference/provider-guide.md` (Steps 1–7)
> **UX Guide**: `docs/design/`

Implement one stage at a time per the approved spec. Each stage's doc is
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

Per stage: `mise run all` passes, no regressions.

---

## Stage 4: Verify

### 4a. Run Smoke Tests

Execute every smoke test command from the Stage 2d verification plan against
a real Grafana instance. Record results (pass/fail + output).

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
`llm_hint` for medium/large) — the `TestConsistency_*` and `TestAgentConformance_*`
suites in `cmd/gcx/root/` fail CI otherwise.

**Build**: `mise run all`, `gcx providers list` lists it, `config view` redacts.

### 4c. Update Architecture Docs

Follow `docs/reference/doc-maintenance.md` structural checks — a new provider
adds packages to `internal/` and commands to `cmd/`, so architecture docs
need updating.

### Gate: All Green

All smoke tests pass, all checklists green, docs updated.

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
