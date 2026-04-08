---
name: add-provider
description: Use when adding a new Grafana Cloud product provider to gcx (SLO, OnCall, Synthetic Monitoring, k6, ML, etc.), or when the user says "add provider", "new provider", or "integrate [product]".
---

# Add Provider

Orchestrates adding a new Grafana product provider — from API discovery through
verified implementation. Four stages with human approval gates.

## When to Use

- User wants to add CLI support for a Grafana Cloud product
- User says "add provider", "new provider", "integrate [product]"
- A bead task references provider implementation

**When NOT to use**: If the product exposes a K8s-compatible `/apis` endpoint,
it already works with `gcx resources` — no provider needed.

**First**: Check `references/decision-tree.md` to confirm a provider is the
right approach.

## Workflow

```
Discover ──gate──> Design ──gate──> Implement ──gate──> Verify
   │                  │                  │                  │
   v                  v                  v                  v
research report    ADRs + spec       code per stage     smoke tests
```

| Stage | Deliverable | Gate |
|-------|-------------|------|
| 1. Discover | `docs/research/` report | User approves findings |
| 2. Design | ADRs + spec + smoke test plan | User approves design |
| 3. Implement | Code (one stage at a time) | `make all` passes per stage |
| 4. Verify | Smoke tests + architecture doc updates | All checks green |

### Prerequisites

Confirm with the user before starting:
- **Product name** — which Grafana product to integrate
- **Access** — do they have a running Grafana instance with the product enabled?
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

Present the research report. Do not proceed to design until approved.

---

## Stage 2: Design

> **Guide**: `docs/reference/provider-discovery-guide.md` Section 2

### 2a. Design Decisions

Answer each decision from the guide, grounded in research findings:

1. **Auth strategy** — reuse Grafana token or separate credentials?
2. **Client type** — plugin API, K8s API, or external service?
3. **Envelope mapping** — how do API objects map to K8s envelope?
4. **Command surface** — CRUD + which beyond-CRUD commands?
5. **Package layout** — flat or subpackaged?
6. **Staging** — how to break into shippable stages?

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

Reference implementations for plan structure:
- SLO: `docs/specs/slo-provider/2026-03-04-slo-provider-plan.md`
- Synth: `docs/specs/synth-provider/2026-03-06-synth-provider-plan.md`

### 2d. Write Smoke Test Plan

**Every stage doc MUST include a Verification section** with concrete smoke
test commands using real values (not placeholders). These are executed in
Stage 4 after implementation.

Example pattern (replace with real product/resource names in actual spec):
```bash
# Provider appears in list
gcx providers | grep {name}

# Config secrets are redacted
gcx config view | grep {name}

# CRUD operations work
gcx {name} {resource} list
gcx {name} {resource} get <test-id>
gcx {name} {resource} push ./testdata/{resource}.yaml
gcx {name} {resource} pull -d ./tmp/
gcx {name} {resource} delete <test-id> --yes

# Unified resources path works
gcx resources get {alias}
```

### Gate: User Approves Design

Present ADRs and spec. Do not proceed to implementation until approved.

---

## Stage 3: Implement

> **Guide**: `docs/reference/provider-guide.md` (Steps 1–7)
> **UX Guide**: `docs/design/`

Implement one stage at a time per the approved spec. Each stage's doc is
self-contained enough to resume in a fresh session.

If `/build-spec` or `/build-task` skills are available, use them to drive
implementation. Otherwise, follow `provider-guide.md` Steps 1–7 directly.
Summary of the key steps:

1. Provider interface + `init()` + `configLoader` (copy from SLO reference)
2. Config keys + validation
3. Commands with UX compliance
4. Types + client + adapter per resource type
5. Register (blank import + ResourceAdapter)
6. Tests (interface compliance, adapter round-trip, client httptest)

**Key patterns** (see provider-guide.md for details):
- Hand-roll HTTP client (~200 LOC) — don't use generated OpenAPI clients
- Copy full `configLoader` from `internal/providers/slo/provider.go`
- Config key names use hyphen-case
- Adapter must strip server-generated fields on Create/Update

### Gate: Stage Complete

Per stage: `make all` passes, no regressions.

---

## Stage 4: Verify

### 4a. Run Smoke Tests

Execute every smoke test command from the Stage 2d verification plan against
a real Grafana instance. Record results (pass/fail + output).

### 4b. Run Checklists

From `docs/design/provider-checklist.md` and `docs/reference/provider-guide.md`:

**Interface**: All 5 Provider methods, `Name()` lowercase/unique, ConfigKeys
complete, secrets marked, Validate returns actionable errors, blank import added.

**UX**: `-o json/yaml` support, text table default, actionable error suggestions,
no `os.Exit()`, cmdio status messages, help text standards, push idempotent,
format-agnostic data fetching, promql-builder for PromQL.

**Build**: `make all`, `gcx providers` lists it, `config view` redacts.

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

Spec plans: `docs/specs/slo-provider/`, `docs/specs/synth-provider/`

## Common Pitfalls

| Pitfall | Mitigation |
|---------|------------|
| K8s CRDs not externally accessible | Verify with real API call before choosing K8s client |
| Incomplete OpenAPI specs | Cross-reference with source code route handlers |
| configLoader is non-trivial | Copy full impl from SLO, don't simplify |
| Missing blank import | Add `_ ".../{name}"` in `cmd/gcx/root/command.go` |
| readOnly fields in POST/PUT | Adapter must strip server-generated fields |
