---
name: migrate-provider
description: Use when porting a Grafana Cloud product from grafana-cloud-cli (gcx) to gcx, when a bead task references gcx provider migration, or when user says "migrate provider", "port from gcx", "port oncall", "port k6". Not for building providers from scratch — use /add-provider for that.
---

# Migrate Provider from gcx

Port an existing gcx resource client into a gcx provider — core adapter,
schema/example registration, CRUD redirect commands, and ancillary subcommands.

**Before starting:** Read `gcx-provider-recipe.md` front to back.
The recipe is the source of truth for mechanical steps. This skill wraps it
with workflow discipline and orchestration.

**Canonical reference:** `internal/providers/incidents/` — the first full port
(adapter + schema + commands + ancillary). Start there for patterns.

## When to Use

- Porting a gcx resource client to gcx
- A bead task references gcx provider migration
- User says "migrate provider", "port from gcx", "port oncall", "port k6"

**When NOT to use**: Building a provider from scratch for a product without
a gcx client — use `/add-provider` instead.

## Prerequisites

Before invoking this skill, ensure:

1. **gcx binary available** — `gcx --version` must succeed. If not installed,
   ask the user for the path or install instructions.
2. **Grafana context configured** — `gcx config view` must show a
   working context with server URL and token. The same context name should
   work for both `gcx --context=<ctx>` and `gcx --context=<ctx>`.
3. **Provider directory exists** — run `/add-dir internal/providers/{name}`
   (or create manually) before starting the port. The directory structure
   must follow the package map in CLAUDE.md.
4. **Live API access** — smoke tests (Stage 3: Verify) require a real Grafana
   instance. Verify connectivity: `gcx --context=<ctx> resources schemas`.

## Pipeline Overview

```
Stage 1: Audit  → read gcx, produce three artifacts, get user approval
Stage 2: Build  → implement provider following recipe, guarded by make all
Stage 3: Verify → run verification plan, produce comparison report, get user approval
```

| Stage | Agent Strategy | Receives | Produces | Gate |
|-------|---------------|----------|----------|------|
| Audit | Lead (main context) | gcx source | Parity table, arch mapping, verification plan | User approves all 3 artifacts |
| Build | Agent team (Core + Commands) | Build envelope | Provider code | `make all` passes |
| Verify | Subagent (fire-and-forget) | Verify envelope | Comparison report + recipe update | User approves report |

Stages are **strictly sequential**. Each stage is separated by a gate that
**must pass** before the next stage begins. Gates are not optional.

> **Small-provider footnote:** For trivially small providers (1-2 subcommands),
> the lead MAY collapse Build-Core and Build-Commands into a single subagent
> instead of an agent team. Document this choice in the migration PR description.

---

## Stage 1: Audit

The Audit stage runs in the lead orchestrator's main context (not delegated).
The lead reads the gcx source, maps every subcommand to its gcx
equivalent, translates gcx patterns to gcx patterns, and writes a
verification plan before any provider code is written. All three artifacts
must be reviewed and approved by the user before Stage 2 begins.

The Audit stage produces two sealed envelopes:
- **Build envelope** — contains the parity table, architectural mapping, and
  a reference to `gcx-provider-recipe.md`. Passed to Build teammates.
- **Verify envelope** — contains the verification plan (test list, smoke
  commands, pass criteria). Passed to the Verify subagent. The Build stage
  must never see this envelope.

### Audit: Artifacts

Produce all three artifacts using the templates in `templates/audit-artifacts.md`.
Fill in concrete values — no placeholders allowed. Do not begin Stage 2 until
all three are complete and the user has approved them.

1. **Parity Table** — one row per gcx subcommand. **Every** gcx subcommand
   for the target provider must appear — no silent omissions.
2. **Architectural Mapping** — concrete translations for all five pattern pairs
   (flat client → TypedCRUD[T], CLI flags → Options, output → codec, types →
   omitzero, registration → init()).
3. **Verification Plan** — specific test names, concrete smoke commands, build
   gate checkpoints. No placeholders.

### Audit: Checklist

- [ ] gcx source read in full — every subcommand identified
- [ ] Parity table complete — every gcx subcommand has a row with status and notes
- [ ] Architectural mapping complete — all five gcx→gcx pattern pairs translated
- [ ] Verification plan complete — specific test names, concrete smoke commands (no placeholders), build gate checkpoints
- [ ] All three artifacts presented to the user
- [ ] User has explicitly approved all three artifacts
- [ ] Build envelope sealed: parity table + architectural mapping + recipe reference
- [ ] Verify envelope sealed: verification plan only (NO parity table, NO arch mapping)

### Audit Gate

> **STOP.** Do not begin Stage 2 (Build) until all three conditions are met:
>
> 1. The **parity table** is complete (every gcx subcommand has a row with
>    status and notes — no silent omissions).
> 2. The **architectural mapping** is complete (all five gcx→gcx
>    pattern pairs are translated explicitly).
> 3. The **verification plan** is complete (specific test names, concrete
>    smoke commands, and build gate checkpoints — no placeholders).
>
> **User approval of all three artifacts is required before proceeding.**
> Present all three to the user and wait for explicit approval.

---

## Stage 2: Build

The Build stage receives only the Build envelope (parity table, architectural
mapping, recipe reference). It must not reference or contain verification plan
content. The Build stage follows `gcx-provider-recipe.md` internal phases
(types, client, adapter, resource_adapter, provider, commands) with a
`make lint` checkpoint between each phase.

The Build stage uses an agent team with two teammates:
- **Build-Core** — owns types, client, adapter, resource_adapter files.
  Must complete before Build-Commands begins.
- **Build-Commands** — owns provider registration and CLI command files.
  Starts only after Build-Core signals completion via the shared TaskList.

Each teammate receives only the Build envelope in its spawn prompt. Neither
teammate receives any verification plan content.

### Build Envelope

**Receives:**
- Parity table (the completed, user-approved artifact from Audit)
- Architectural mapping (the completed, user-approved artifact from Audit)
- Recipe reference: "Follow `gcx-provider-recipe.md` Steps 2-8 for mechanical
  implementation steps. The recipe is authoritative for file structure, client
  pattern, adapter wiring, and registration."

**Produces:**
- All provider implementation files within the teammate's ownership boundary
- Confirmation message to the lead via TeamSendMessage when work is complete

**Enforcement:** The lead orchestrator's spawn prompt for each Build teammate
contains ONLY the three items listed under Receives. The spawn prompt must
NOT include: the verification plan, smoke test commands, expected comparison
outputs, or any other Verify envelope content.

### Build: Orchestration

```bash
# 1. Create the team
TeamCreate("build-{provider}")

# 2. Spawn Build-Core (Agent tool with team_name)
#    Use spawn prompt from templates/spawn-prompts.md → Build-Core

# 3. Wait for Build-Core to signal completion via TaskList.
#    DO NOT spawn Build-Commands until Build-Core task is marked complete.

# 4. Spawn Build-Commands (Agent tool with team_name)
#    Use spawn prompt from templates/spawn-prompts.md → Build-Commands

# 5. Wait for both teammates to complete.

# 6. Run BUILD GATE: GCX_AGENT_MODE=false make all

# 7. Tear down: TeamDelete("build-{provider}")
```

### File Ownership Table

| Recipe Phase | File(s) | Teammate |
|---|---|---|
| Step 2: Types | `internal/providers/{name}/types.go` | Build-Core |
| Step 3: Client | `internal/providers/{name}/client.go`, `client_test.go` | Build-Core |
| Step 4: Adapter | `internal/providers/{name}/adapter.go` | Build-Core |
| Step 5: Resource Adapter | `internal/providers/{name}/resource_adapter.go` | Build-Core |
| Step 6: Provider registration | `internal/providers/{name}/provider.go` | Build-Commands |
| Step 7: CLI Commands | `cmd/gcx/providers/{name}/commands.go`, `*_test.go` | Build-Commands |
| Blank import | `cmd/gcx/root/command.go` (import line only) | Build-Commands |

Teammates MUST NOT modify files outside their ownership boundary.

### Build: Checklist

**Build-Core:**
- [ ] `types.go`: all gcx types translated; struct fields use `omitzero` (not `omitempty`)
- [ ] `types.go`: `make lint` passes
- [ ] `client.go`: HTTP client following recipe Step 3 (no embedded `*grafana.Client`)
- [ ] `client_test.go`: httptest tests for List, Get, Create, and any other CRUD ops
- [ ] `client.go`: `make lint` passes
- [ ] `adapter.go`: `TypedCRUD[T]` wired with all five functions + `NameFn`
- [ ] `adapter.go`: `make lint` passes
- [ ] `resource_adapter.go`: `ResourceAdapter` interface implemented; adapter registered
- [ ] `resource_adapter.go`: `make lint` passes
- [ ] TaskList: Build-Core task marked **complete**; lead notified

**Build-Commands:**
- [ ] TaskList: confirmed Build-Core task is **complete** before starting
- [ ] `provider.go`: `providers.Register()` and all resource `adapter.Register()` calls in `init()`
- [ ] `provider.go`: `make lint` passes
- [ ] `commands.go`: all **Implemented** commands from parity table have subcommands
- [ ] `commands.go`: each command uses Options struct with `setup()` and `Validate()`
- [ ] `commands.go`: output routed through codec registry (`-o table/wide/json/yaml`)
- [ ] Command tests: at least one test per command via httptest
- [ ] `commands.go`: `make lint` passes
- [ ] Blank import line added to `cmd/gcx/root/command.go`

### Build Gate

> **STOP.** Do not begin Stage 3 (Verify) until:
>
> `GCX_AGENT_MODE=false make all` exits 0 with no lint errors and
> all tests passing.
>
> Run this command after both Build teammates complete. If it fails, fix
> the root cause before proceeding — do not proceed with a failing build.

---

## Stage 3: Verify

The Verify stage runs as a subagent that receives only the Verify envelope
(verification plan). The subagent must not reference Build-stage implementation
details (internal function names, error handling approach, test structure
chosen by the builder). It executes every item in the verification plan and
produces a structured comparison report.

### Verify Envelope

**Receives:**
- Verification plan (the completed, user-approved artifact from Audit)

**Produces:**
- Structured comparison report (template in `templates/comparison-report.md`)
- Updates to `gcx-provider-recipe.md` (new gotchas, pattern corrections,
  status tracker entry for the ported provider)

> **FR-011 — Recipe update is MANDATORY.** The Verify stage MUST update
> `gcx-provider-recipe.md` before completing:
> 1. **Status tracker entry** — add a row for the ported provider.
> 2. **Gotchas section** — record any problems discovered during smoke tests.
>
> Do not pass the Verify gate without making both updates, even if no new
> gotchas were found (record "No new gotchas" explicitly).

**Enforcement:** The lead orchestrator's spawn prompt for the Verify subagent
contains ONLY the verification plan. The spawn prompt must NOT include: the
parity table, architectural mapping, recipe reference, internal function names
chosen during Build, or any other Build envelope content.

**Spawn prompt template:** `templates/spawn-prompts.md` → Verify Spawn Prompt

### Verify Gate

> **STOP.** Do not declare the migration complete until:
>
> 1. The **comparison report** has been produced and presented to the user.
> 2. Every discrepancy in the report is either:
>    - **Justified** with a written rationale explaining why the difference
>      is acceptable, or
>    - **Fixed** and the fix verified.
>
> **User review of the comparison report is required.** The user must
> explicitly approve the report or request fixes before this gate passes.

---

## Red Flags — STOP and Check

When you notice any of these during execution, stop and take the corrective
action before continuing. The "rationalization" column shows the excuse agents
typically generate — if you catch yourself thinking this, it's a red flag.

| Red Flag | Rationalization | STOP. Do this instead |
|---|---|---|
| **Copying gcx client verbatim** — embedding `*grafana.Client`, using `c.Get()`/`c.Post()` directly | "The gcx client already works, adapting it would just introduce bugs" | Translate to a typed HTTP client (plain `http.Client` + named endpoint methods). Read recipe Step 3 for the gcx client pattern. |
| **Skipping the parity audit** — jumping to implementation | "I can see the important commands from the gcx source, a full table would be redundant" | The parity table is required. Every gcx subcommand must have a row. Unaudited subcommands become missing features. Return to Stage 1 and complete the table. |
| **Guessing endpoint names or paths** — using `/api/v1/resources` when actual path is `/api/v1/orgs/{id}/resources` | "The endpoint pattern is obvious from the resource name" | Read the gcx source for exact paths. Run `gcx --context=$CTX {resource} list --help` to confirm. Never guess paths. |
| **Skipping smoke tests** — marking Verify "complete" without running commands | "The unit tests pass, so the implementation is correct" | Smoke tests are required. If no live instance is available, block and tell the user. Do not pass the Verify gate without running every item in the verification plan. |
| **Peeking at the verification plan during Build** — reading it "to make sure the implementation will pass" | "I need to check the verification plan to make sure my implementation will pass the smoke tests" | Your context is the Build envelope only. Unit tests must be derived from requirements (parity table + arch mapping), not from knowledge of what smoke tests will run. |
| **Peeking at implementation during Verify** — reading adapter code "to understand what I'm testing" | "Let me look at the adapter code to understand what I'm testing so I can write better checks" | You test behavior, not structure. Derive all expected behavior from the verification plan. If the plan is insufficient, that's a plan quality issue — report it, don't compensate by reading Build artifacts. |
| **Merging envelopes** — passing both the parity table AND the verification plan to a Build teammate | "Giving the builder more context will help them write better code" | Each teammate receives only its designated envelope. The isolation is the mechanism that prevents overfitting. |
| **Build-Commands starting before Build-Core signals** — importing adapter interfaces that do not exist yet | "I can start on the command structure while Build-Core finishes the types" | Check the TaskList. Wait for the Build-Core task to be marked **complete** before writing any command that imports the adapter. |
| **Unit tests derived from smoke commands** — writing test cases based on the verification plan | "I'll use the smoke test commands as a guide for what to test in unit tests" | Unit tests must be derived from the parity table and architectural mapping only. Do not read the verification plan during Build. |
