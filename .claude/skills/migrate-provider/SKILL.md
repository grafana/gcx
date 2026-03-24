---
name: migrate-provider
description: Use when porting a Grafana Cloud product from grafana-cloud-cli (gcx) to grafanactl, when a bead task references gcx provider migration, or when user says "migrate provider", "port from gcx", "port oncall", "port k6". Not for building providers from scratch — use /add-provider for that.
---

# Migrate Provider from gcx

Port an existing gcx resource client into a grafanactl provider — core adapter,
schema/example registration, CRUD redirect commands, and ancillary subcommands.

**Before starting:** Read `gcx-provider-recipe.md` front to back.
The recipe is the source of truth for mechanical steps. This skill wraps it
with workflow discipline and orchestration.

**Canonical reference:** `internal/providers/incidents/` — the first full port
(adapter + schema + commands + ancillary). Start there for patterns.

## Prerequisites

Before invoking this skill, ensure:

1. **gcx binary available** — `gcx --version` must succeed. If not installed,
   ask the user for the path or install instructions.
2. **Grafana context configured** — `grafanactl config view` must show a
   working context with server URL and token. The same context name should
   work for both `grafanactl --context=<ctx>` and `gcx --context=<ctx>`.
3. **Provider directory exists** — run `/add-dir internal/providers/{name}`
   (or create manually) before starting the port. The directory structure
   must follow the package map in CLAUDE.md.
4. **Live API access** — smoke tests (Stage 3: Verify) require a real Grafana
   instance. Verify connectivity: `grafanactl --context=<ctx> resources schemas`.

## Pipeline Overview

```
Stage 1: Audit  → read gcx, produce three artifacts, get user approval
Stage 2: Build  → implement provider following recipe, guarded by make all
Stage 3: Verify → run verification plan, produce comparison report, get user approval
```

Stages are **strictly sequential**. Each stage is separated by a gate that
**must pass** before the next stage begins. Gates are not optional.

---

## Stage 1: Audit

The Audit stage runs in the lead orchestrator's main context (not delegated).
The lead reads the gcx source, maps every subcommand to its grafanactl
equivalent, translates gcx patterns to grafanactl patterns, and writes a
verification plan before any provider code is written. All three artifacts
must be reviewed and approved by the user before Stage 2 begins.

The Audit stage produces two sealed envelopes:
- **Build envelope** — contains the parity table, architectural mapping, and
  a reference to `gcx-provider-recipe.md`. Passed to Build teammates.
- **Verify envelope** — contains the verification plan (test list, smoke
  commands, pass criteria). Passed to the Verify subagent. The Build stage
  must never see this envelope.

### Audit: Artifacts

Produce all three artifacts in order. Do not begin Stage 2 until all three
are complete and the user has approved them.

#### Artifact 1: Parity Table

Copy this template and fill in one row per gcx subcommand. **Every** gcx
subcommand for the target provider must appear — no silent omissions.

```markdown
## Parity Table: {provider} ({gcx source path})

| gcx command | grafanactl equivalent | status | notes |
|-------------|-----------------------|--------|-------|
| {resource} list | grafanactl {resource} list | Implemented | Maps to adapter ListFn |
| {resource} get {id} | grafanactl {resource} get {id} | Implemented | Maps to adapter GetFn |
| {resource} create | grafanactl {resource} create | Implemented | Maps to adapter CreateFn |
| {resource} update {id} | grafanactl {resource} update {id} | Implemented | Maps to adapter UpdateFn |
| {resource} delete {id} | grafanactl {resource} delete {id} | Implemented | Maps to adapter DeleteFn |
| {resource} {subcommand} | grafanactl {resource} {subcommand} | Deferred / N/A | {reason} |

Status values: Implemented | Deferred | N/A
```

#### Artifact 2: Architectural Mapping

Copy this template and fill in concrete translations for all five pattern
pairs. Every pair must have an entry — do not omit any.

```markdown
## Architectural Mapping: {provider}

### (a) gcx flat client → TypedCRUD[T] adapter

gcx pattern:
  type Client struct { *grafana.Client }
  func (c *Client) ListResources(ctx) ([]T, error) { c.Get(...) }

grafanactl translation:
  adapter.TypedCRUD[{ResourceType}]{
    ListFn:   client.List,
    GetFn:    client.Get,
    CreateFn: client.Create,
    UpdateFn: client.Update,
    DeleteFn: client.Delete,
    NameFn:   func(r {ResourceType}) string { return r.{UID field} },
  }

Notes: {any provider-specific adaptations, e.g. int→string ID mapping}

### (b) gcx CLI flags → Options struct with setup/Validate

gcx pattern:
  cmd.Flags().StringVar(&opts.Filter, "filter", "", "...")
  // ad-hoc validation inline in RunE

grafanactl translation:
  type {Resource}Opts struct { Filter string }
  func (o *{Resource}Opts) setup(cmd *cobra.Command) { ... }
  func (o *{Resource}Opts) Validate() error { ... }

Notes: {list each flag that needs translation}

### (c) gcx output formatting → codec registry with K8s envelope

gcx pattern:
  json.Marshal(result) / fmt.Printf table directly

grafanactl translation:
  codec.Encode(resources, opts.Output) where resources is []*Resource
  wrapped in K8s envelope: TypeMeta{Kind, APIVersion} + ObjectMeta{Name}
  Output modes: table (default), wide, json, yaml

Notes: {any fields used as table columns, any wide-only columns}

### (d) gcx types → Go structs with omitzero

gcx pattern:
  type Resource struct { Field *string `json:"field,omitempty"` }

grafanactl translation:
  type Resource struct { Field string `json:"field,omitzero"` }
  (Go 1.24+ omitzero replaces omitempty for struct-typed fields)

Notes: {list any FlexTime or special zero-value fields}

### (e) gcx provider registration → adapter.Register() in init() with blank import

gcx pattern:
  // registration in main package or explicit wire-up

grafanactl translation:
  // internal/providers/{name}/provider.go
  func init() {
    providers.Register(&Provider{})
    {resource}.Register(&configLoader{})
  }
  // cmd/grafanactl/root/command.go
  _ "github.com/grafana/grafanactl/internal/providers/{name}"

Notes: {ConfigKeys required: [] for same SA token, [{Name: "url"}, {Name: "token"}] for separate}
```

#### Artifact 3: Verification Plan

Copy this template and fill in concrete values. No placeholders allowed —
use actual resource names, field names, and context names.

```markdown
## Verification Plan: {provider}

### Automated Tests

1. Client HTTP tests (`{resource}/client_test.go`):
   - `Test{Resource}Client_List` — httptest server returning known JSON fixture,
     verify all fields parse correctly
   - `Test{Resource}Client_Get` — httptest returning single resource, verify
     fields including nested structs
   - `Test{Resource}Client_Create` — verify request body and response parsing
   - `Test{Resource}Client_Error` — 4xx/5xx responses produce wrapped errors

2. Adapter round-trip tests (`{resource}/adapter_test.go`):
   - `Test{Resource}AdapterRoundTrip` — create typed object → adapter wraps to
     Resource → unwrap back → compare all fields (no data loss)

3. TypedCRUD interface compliance:
   - Compilation gate: if `adapter.TypedCRUD[{ResourceType}]` does not satisfy
     the `ResourceAdapter` interface, `make build` will catch it

### Smoke Test Commands

Run every command with CTX={context-name} against the live instance.

```bash
CTX={context-name}  # fill in before running

# --- List: compare resource IDs ---
GCX_IDS=$(gcx --context=$CTX {resource} list -o json | jq -r '.[].{id_field}' | sort)
GCTL_IDS=$(grafanactl --context=$CTX {resource} list -o json | jq -r '.[].metadata.name' | sort)
echo "=== List ID diff ===" && diff <(echo "$GCX_IDS") <(echo "$GCTL_IDS") && echo "MATCH" || echo "MISMATCH"

# --- Get: compare key fields ---
ID="{pick a real ID from list output}"
gcx --context=$CTX {resource} get $ID -o json \
  | jq '{title: .{title_field}, status: .{status_field}}' > /tmp/gcx_get.json
grafanactl --context=$CTX {resource} get $ID -o json \
  | jq '{title: .spec.{title_field}, status: .spec.{status_field}}' > /tmp/gctl_get.json
echo "=== Get field diff ===" && diff /tmp/gcx_get.json /tmp/gctl_get.json && echo "MATCH" || echo "MISMATCH"

# --- Adapter path ---
grafanactl --context=$CTX resources get {alias} > /dev/null 2>&1 && echo "resources get: OK" || echo "resources get: FAIL"
grafanactl --context=$CTX resources get {alias}/$ID -o json > /dev/null 2>&1 && echo "resources get/id: OK" || echo "resources get/id: FAIL"

# --- Ancillary subcommands (one block per non-CRUD subcommand) ---
echo "=== Ancillary: {subcommand} ===" && \
gcx --context=$CTX {resource} {subcommand} -o json | jq length && \
grafanactl --context=$CTX {resource} {subcommand} -o json | jq length

# --- Output format check ---
for fmt in table wide json yaml; do
  GRAFANACTL_AGENT_MODE=false grafanactl --context=$CTX {resource} list -o $fmt > /dev/null 2>&1 \
    && echo "$fmt: OK" || echo "$fmt: FAIL"
done
```

### Build Gate Checkpoints

Run `GRAFANACTL_AGENT_MODE=false make all` at these points:
1. After Step 2 (types.go) — verify compilation
2. After Step 3 (client.go) — verify lint passes
3. After Step 4 (adapter.go) — verify TypedCRUD wiring compiles
4. After Step 6 (tests) — verify all tests pass
5. **Final gate** before Stage 3: `GRAFANACTL_AGENT_MODE=false make all` must
   exit 0 with no lint errors, all tests passing, and docs regenerated.
```

### Audit Gate

> **STOP.** Do not begin Stage 2 (Build) until all three conditions are met:
>
> 1. The **parity table** is complete (every gcx subcommand has a row with
>    status and notes — no silent omissions).
> 2. The **architectural mapping** is complete (all five gcx→grafanactl
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

**Description:** The Build envelope is the complete context a Build teammate
receives in its spawn prompt. It contains everything needed to implement the
provider and nothing from the Verify stage. The lead orchestrator constructs
this envelope after the Audit gate passes and before spawning the Build team.

**Receives:**
- Parity table (the completed, user-approved artifact from Audit)
- Architectural mapping (the completed, user-approved artifact from Audit)
- Recipe reference: "Follow `gcx-provider-recipe.md` Steps 1-6 for mechanical
  implementation steps. The recipe is authoritative for file structure, client
  pattern, adapter wiring, and registration."

**Produces:**
- All provider implementation files within the teammate's ownership boundary
  (see file ownership table below)
- Confirmation message to the lead via TeamSendMessage when work is complete

**Enforcement:** The lead orchestrator's spawn prompt for each Build teammate
contains ONLY the three items listed under Receives. The spawn prompt must
NOT include: the verification plan, smoke test commands, expected comparison
outputs, or any other Verify envelope content. The teammate runs in its own
agent session and has access only to what is in its spawn prompt plus the
repository files it reads.

### Build: Orchestration

#### Agent Team Setup

```bash
# 1. Create the team
TeamCreate("build-{provider}")

# 2. Spawn Build-Core (Agent tool with team_name — see spawn prompt below)
#    Build-Core starts immediately.

# 3. Wait for Build-Core to signal completion via TaskList.
#    DO NOT spawn Build-Commands until Build-Core task is marked complete.

# 4. Spawn Build-Commands (Agent tool with team_name — see spawn prompt below)

# 5. Wait for both teammates to complete.

# 6. Run BUILD GATE: GRAFANACTL_AGENT_MODE=false make all

# 7. Tear down: TeamDelete("build-{provider}")
```

#### File Ownership Table

| Recipe Phase | File(s) | Teammate |
|---|---|---|
| Step 2: Types | `internal/providers/{name}/types.go` | Build-Core |
| Step 3: Client | `internal/providers/{name}/client.go`, `client_test.go` | Build-Core |
| Step 4: Adapter | `internal/providers/{name}/adapter.go` | Build-Core |
| Step 5: Resource Adapter | `internal/providers/{name}/resource_adapter.go` | Build-Core |
| Step 6: Provider registration | `internal/providers/{name}/provider.go` | Build-Commands |
| Step 7: CLI Commands | `cmd/grafanactl/providers/{name}/commands.go`, `*_test.go` | Build-Commands |
| Blank import | `cmd/grafanactl/root/command.go` (import line only) | Build-Commands |

Teammates MUST NOT modify files outside their ownership boundary.

#### Build-Core Spawn Prompt Template

```
You are Build-Core for the {provider} provider migration.

## Your Task

Implement the core adapter files for the {provider} provider.

**You own ONLY these files:**
- `internal/providers/{name}/types.go`
- `internal/providers/{name}/client.go`
- `internal/providers/{name}/client_test.go`
- `internal/providers/{name}/adapter.go`
- `internal/providers/{name}/resource_adapter.go`

Do NOT create or modify provider.go or any CLI command files. Those are owned by Build-Commands.

## Build Envelope

### Parity Table

{paste the completed parity table here}

### Architectural Mapping

{paste the completed architectural mapping here}

### Recipe Reference

Follow `gcx-provider-recipe.md` Steps 2-5 (types, client, adapter, resource_adapter)
for mechanical implementation steps. The recipe is authoritative for file structure,
client pattern, adapter wiring, and registration patterns.

## Completion

When all files are implemented and `make lint` passes on your files:
1. Mark the Build-Core task complete via TaskList.
2. Send a message to the lead confirming completion and listing the files you created.
```

#### Build-Commands Spawn Prompt Template

```
You are Build-Commands for the {provider} provider migration.

## Your Task

Implement the provider registration and CLI commands for the {provider} provider.
The core adapter (types, client, adapter, resource_adapter) has already been
implemented by Build-Core.

**You own ONLY these files:**
- `internal/providers/{name}/provider.go`
- `cmd/grafanactl/providers/{name}/commands.go`
- `cmd/grafanactl/providers/{name}/*_test.go` (command tests)
- The blank import line in `cmd/grafanactl/root/command.go`

Do NOT modify types.go, client.go, adapter.go, or resource_adapter.go.
Those are owned by Build-Core.

## Build Envelope

### Parity Table

{paste the completed parity table here}

### Architectural Mapping

{paste the completed architectural mapping here}

### Recipe Reference

Follow `gcx-provider-recipe.md` Steps 6-8 (provider registration, CLI commands)
for mechanical implementation steps. The recipe is authoritative for command
patterns, Options structs, and codec usage.

## Notes

- The adapter interfaces are already implemented by Build-Core. Import and use them;
  do not modify them.
- Before starting any command implementation that imports the adapter, confirm that
  the Build-Core task is marked complete in TaskList.

## Completion

When all files are implemented and `make lint` passes on your files:
1. Send a message to the lead confirming completion and listing the files you created.
```

### Build: Checklist

#### Build-Core Checklist

- [ ] `types.go`: all gcx types translated; struct fields use `omitzero` (not `omitempty`)
- [ ] `types.go`: `make lint` passes
- [ ] `client.go`: HTTP client implemented following recipe Step 3 pattern (no embedded `*grafana.Client`)
- [ ] `client_test.go`: httptest server tests for List, Get, Create, and any other CRUD ops in parity table
- [ ] `client.go`: `make lint` passes
- [ ] `adapter.go`: `TypedCRUD[T]` wired with all five functions (ListFn, GetFn, CreateFn, UpdateFn, DeleteFn)
- [ ] `adapter.go`: `NameFn` set to return the resource UID / name field
- [ ] `adapter.go`: `make lint` passes
- [ ] `resource_adapter.go`: `ResourceAdapter` interface implemented; adapter registered
- [ ] `resource_adapter.go`: `make lint` passes
- [ ] TaskList: Build-Core task marked **complete**; lead notified

#### Build-Commands Checklist

- [ ] TaskList: confirmed Build-Core task is **complete** before starting
- [ ] `provider.go`: `providers.Register()` and all resource `adapter.Register()` calls in `init()`
- [ ] `provider.go`: `make lint` passes
- [ ] `commands.go`: all **Implemented** commands from parity table have subcommands
- [ ] `commands.go`: each command uses an Options struct with `setup()` and `Validate()`
- [ ] `commands.go`: output routed through codec registry (`-o table/wide/json/yaml`)
- [ ] Command tests: at least one test per command via httptest
- [ ] `commands.go`: `make lint` passes
- [ ] Blank import line added to `cmd/grafanactl/root/command.go`

### Build Gate

> **STOP.** Do not begin Stage 3 (Verify) until:
>
> `GRAFANACTL_AGENT_MODE=false make all` exits 0 with no lint errors and
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
produces a structured comparison report. After the report is complete, it
updates `gcx-provider-recipe.md` with any new discoveries (gotchas, pattern
corrections, status tracker entry).

### Verify Envelope

**Description:** The Verify envelope is the complete context the Verify
subagent receives in its spawn prompt. It contains the verification plan and
nothing from the Build stage. The lead orchestrator constructs this envelope
during Audit and seals it — the Verify subagent is spawned with this content
only after the Build gate passes.

**Receives:**
- Verification plan (the completed, user-approved artifact from Audit)

**Produces:**
- Structured comparison report (see Comparison Report template below)
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
chosen during Build, error handling approach, test structure, or any other
Build envelope content. The subagent runs in its own agent session and derives
all expected behavior from the verification plan, not from knowledge of how
the Build stage implemented things.

### Verify: Spawn Prompt

```
You are the Verify agent for the {provider} provider migration.

## Your Task

Execute the verification plan below and produce a structured comparison report.

You have access ONLY to the verification plan in this prompt. Do NOT reference any
Build-stage implementation details (internal function names, error handling approach,
test structure chosen by the builder). Derive all expected behavior from the
verification plan.

## Verify Envelope

### Verification Plan

{paste the completed verification plan here — test list, smoke commands, pass criteria}

## Deliverables

1. **Comparison report** — fill in the template in Stage 3: Verify → Comparison Report
   and present it to the user.

2. **Recipe update (REQUIRED — FR-011)** — after the report is complete, you
   MUST update `gcx-provider-recipe.md` with:
   - **Status tracker entry** for this provider (required even if no issues found)
   - **Gotchas** (problems discovered during smoke tests; write "No new gotchas" if none)
   - Pattern corrections (if any recipe step was unclear or incorrect)

Execute every item in the verification plan. Do not skip or abbreviate any step.
```

### Verify: Comparison Report

Copy this template and fill it in for every command in the verification plan.
Every row must have a status. Do not omit commands or mark them "skipped".

```markdown
## Comparison Report: {provider}

### Per-Command Pass/Fail

| command | status | captured output (truncated) |
|---------|--------|-----------------------------|
| gcx {resource} list | PASS / FAIL | {first 3 lines of output or error} |
| grafanactl {resource} list | PASS / FAIL | {first 3 lines of output or error} |
| gcx {resource} get {id} | PASS / FAIL | {first 3 lines} |
| grafanactl {resource} get {id} | PASS / FAIL | {first 3 lines} |
| grafanactl resources get {alias} | PASS / FAIL | {first 3 lines} |
| grafanactl {resource} {subcommand} | PASS / FAIL | {first 3 lines} |

### List ID Comparison

```diff
=== List ID diff ===
{paste full diff output here, or "MATCH" if identical}
```

Verdict: MATCH | MISMATCH
If MISMATCH: {describe which IDs differ and probable cause}

### Get Field Comparison

```diff
=== Get field diff ===
{paste full diff output here, or "MATCH" if identical}
```

Verdict: MATCH | MISMATCH
If MISMATCH: {describe which fields differ — note any acceptable differences
such as computed fields that differ by small values}

### Output Format Check

| format | status | notes |
|--------|--------|-------|
| table | OK / FAIL | {error if FAIL} |
| wide | OK / FAIL | {error if FAIL} |
| json | OK / FAIL | {error if FAIL} |
| yaml | OK / FAIL | {error if FAIL} |

### Discrepancy Summary

| # | description | verdict | rationale or fix |
|---|-------------|---------|-----------------|
| 1 | {describe any mismatch or unexpected behavior} | justified / fix required | {written rationale or PR link} |

(Leave table empty if no discrepancies found.)
```

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

## Orchestration

Each stage uses a different agent strategy. The lead orchestrator manages all
gates between stages — inspecting artifacts, waiting for teammates, reviewing
the comparison report.

| Stage | Agent Strategy | Notes |
|---|---|---|
| Audit | **Main context** (lead orchestrator) | Runs inline. Requires interactive user review and approval of all three artifacts. MUST NOT be delegated to a subagent or teammate. |
| Build | **Agent team** — Build-Core + Build-Commands | Two teammates with disjoint file ownership. Build-Core completes first; Build-Commands waits for TaskList signal. Lead runs the BUILD GATE after both finish. |
| Verify | **Subagent** (Agent tool, fire-and-forget) | Single focused task. Lead passes only the Verify envelope in the spawn prompt. Lead reviews the comparison report when the subagent returns. |

> **Small-provider footnote:** For trivially small providers (1-2 subcommands),
> the lead MAY collapse Build-Core and Build-Commands into a single subagent
> instead of an agent team. Document this choice in the migration PR description.

---

## Audit: Checklist

- [ ] gcx source read in full — every subcommand identified
- [ ] Parity table complete — every gcx subcommand has a row with status and notes
- [ ] Architectural mapping complete — all five gcx→grafanactl pattern pairs translated
- [ ] Verification plan complete — specific test names, concrete smoke commands (no placeholders), build gate checkpoints
- [ ] All three artifacts presented to the user
- [ ] User has explicitly approved all three artifacts
- [ ] Build envelope sealed: parity table + architectural mapping + recipe reference
- [ ] Verify envelope sealed: verification plan only (NO parity table, NO arch mapping)

---

## Red Flags — STOP and Check

When you notice any of these during execution, stop and take the corrective action before continuing.

| Red Flag | STOP. Do this instead |
|---|---|
| **Copying gcx client verbatim** — embedding `*grafana.Client`, using `c.Get()`/`c.Post()` directly | Translate to a typed HTTP client (plain `http.Client` + named endpoint methods). Read recipe Step 3 for the grafanactl client pattern. |
| **Skipping the parity audit** — "I'll just implement the obvious commands" | The parity table is required. Every gcx subcommand must have a row. Unaudited subcommands become missing features. Return to Stage 1 and complete the table. |
| **Guessing endpoint names or paths** — using `/api/v1/resources` when actual path is `/api/v1/orgs/{id}/resources` | Read the gcx source for exact paths. Run `gcx --context=$CTX {resource} list --help` to confirm. Never guess paths. |
| **Skipping smoke tests** — marking Verify "complete" without running commands, or deferring them | Smoke tests are required. If no live instance is available, block and tell the user. Do not pass the Verify gate without running every item in the verification plan. |
| **Reading files outside your spawn prompt** — Build teammate reads `verify-envelope.md`; Verify subagent reads `build-envelope.md` | Your context is your spawn prompt only. Do not read stage envelope files that were not given to you in your spawn prompt. This is the isolation boundary. |
| **Merging envelopes** — passing both the parity table AND the verification plan to a Build teammate | Each teammate receives only its designated envelope. Build teammates receive: parity table + arch mapping + recipe ref. Verify subagent receives: verification plan only. |
| **Build-Commands starting before Build-Core signals** — importing adapter interfaces that do not exist yet | Check the TaskList. Wait for the Build-Core task to be marked **complete** before writing any command that imports the adapter. |
| **Unit tests derived from smoke commands** — writing test cases based on knowledge of what the verification plan will check | Unit tests must be derived from the parity table and architectural mapping only. Do not read the verification plan during Build. |
