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
4. **Live API access** — smoke tests (Phase 5) require a real Grafana
   instance. Verify connectivity: `grafanactl --context=<ctx> resources schemas`.

## Workflow

```
Phase 1:   Pre-flight           → answer 6 questions, locate gcx source
Phase 1.5: Feature parity audit → gcx→grafanactl command mapping table
Phase 2:   Core adapter         → types, client, adapter, resource_adapter, provider
Phase 3:   Schema + example     → register in adapter.Registration
Phase 4:   Provider commands    → CRUD redirects + ancillary subcommands (format-compliant)
Phase 5:   Smoke test + recipe  → STOP gate: structured diff, then update recipe
```

Phases are **sequential**. See Orchestration section for within-phase parallelism.

### Phase 1: Pre-flight

1. Read recipe front to back
2. Run pre-flight checklist (recipe section) — answer all 6 questions
3. Locate gcx source: `pkg/grafana/{resource}/`, `cmd/`
4. Check K8s discovery: `grafanactl resources schemas | grep -i {resource}`
   — if already discovered, no provider needed

### Phase 1.5: Feature Parity Audit

**Before writing any code**, produce a command mapping table covering every
gcx subcommand for this resource. This prevents partial ports and forces
explicit decisions about what to defer.

**Output format** (paste into the bead or PR description):

```
| gcx command                        | grafanactl equivalent          | Status      | Notes                     |
|------------------------------------|--------------------------------|-------------|---------------------------|
| gcx {resource} list                | {resource} list                | Implemented | table+wide+json+yaml      |
| gcx {resource} get <id>            | {resource} get <id>            | Implemented | yaml default, K8s envelope |
| gcx {resource} create -f <file>    | {resource} create -f <file>    | Implemented |                           |
| gcx {resource} delete <id>         | N/A                            | N/A         | API has no delete endpoint |
| gcx {resource} {sub} list          | {resource} {sub} list          | Deferred    | Low usage, Phase 2        |
```

**Rules:**
- Every gcx subcommand gets a row — no silent omissions
- Valid statuses: `Implemented`, `Deferred`, `N/A`
- `Deferred` must include justification and target phase
- `N/A` must explain why (e.g., "API has no endpoint", "superseded by K8s API")
- The table must be reviewed before proceeding to Phase 2

### Phase 2: Core Adapter

Follow recipe steps 1-6. See `conventions.md` for Go-specific gotchas
(omitzero, exported codecs, API group naming, linter traps).

**After Phase 2:** smoke test core adapter path (`resources get {alias}`).

### Phase 3: Schema + Example

Register `Schema` and `Example` (both `json.RawMessage`) in
`adapter.Registration` during `init()`. Build as static maps — no external
deps needed. See `conventions.md` for errchkjson requirements.

**Verify:** `resources schemas {alias} -o json`, `resources examples {alias}`.

### Phase 4: Provider Commands

Implement CRUD redirects (list, get, create, close) and ancillary
subcommands (activity, severities, open, etc.) in `commands.go`. See
`commands-reference.md` for the patterns and code examples.

**CRITICAL:** Always verify API endpoint names against gcx source — don't
guess from naming patterns.

**Output format compliance** (ref: `docs/reference/design-guide.md` Section 1.3):

| Command type | Default format | Required codecs | K8s wrapping |
|-------------|---------------|-----------------|--------------|
| `list` | `text` | `table` + `wide` | json/yaml output must use `ToResource` for K8s envelope |
| `get` | `text` | `table` (single-row) | json/yaml via `ToResource` K8s envelope |
| `create` | Status message | — | Return created resource in json/yaml if `-o` specified |
| Operational / query | Varies | Per-command | Exception: may skip K8s wrapping if data is not a resource |

- `list` and `get` **must** register both `table` and `wide` custom codecs
- `table` codec: key identifying columns (ID/UID, name/title, status)
- `wide` codec: all `table` columns + additional detail (timestamps, labels, counts)
- json/yaml output must wrap through `ToResource` to produce K8s-style envelope
  (`apiVersion`, `kind`, `metadata`, `spec`) — this is what `resources get` uses
- Operational commands (close, open, activity) use status messages, not data output
- Export table codec types (e.g., `IncidentTableCodec`) for `_test` package access

**After Phase 4:** smoke test ALL commands side-by-side with gcx.

### Phase 5: Smoke Test + Update Recipe

> **STOP AND REPORT.** Do not declare this phase complete until you have
> pasted the structured comparison results below into the conversation.
> The user must see evidence that every command produces equivalent output.

**Step 1: Structured comparison for every command.**

For each command the provider exposes, run both gcx and grafanactl and
produce a structured diff. Use jq pipelines to normalize and compare:

```bash
# List — compare IDs
GCX_IDS=$(gcx --context=<ctx> {resource} list -o json | jq -r '.[].id // .[].uid' | sort)
GCTL_IDS=$(grafanactl --context=<ctx> {resource} list -o json | jq -r '.items[].metadata.name' | sort)
diff <(echo "$GCX_IDS") <(echo "$GCTL_IDS")

# Get — compare key fields
gcx --context=<ctx> {resource} get <id> -o json | jq '{title, status, labels}' > /tmp/gcx_get.json
grafanactl --context=<ctx> {resource} get <id> -o json | jq '{title: .spec.title, status: .spec.status, labels: .metadata.labels}' > /tmp/gctl_get.json
diff /tmp/gcx_get.json /tmp/gctl_get.json

# Output formats — verify all four render without errors
for fmt in table wide json yaml; do
  GRAFANACTL_AGENT_MODE=false grafanactl --context=<ctx> {resource} list -o $fmt > /dev/null 2>&1 && echo "$fmt: OK" || echo "$fmt: FAIL"
done
```

**Step 2: Paste results.** Copy the diff output and format-check results
into the conversation. If any diff is non-empty, explain the discrepancy
(acceptable: computed fields like `durationSeconds`; unacceptable: missing
resources, wrong IDs, missing fields).

> **STOP.** Do not proceed to Step 3 until the comparison results are
> pasted and any discrepancies are justified.

**Step 3: Update recipe.** Update `gcx-provider-recipe.md`:
- Provider Status Tracker: mark as done with date
- Gotchas & Lessons Learned: add any new discoveries

**Step 4: Final build.** `GRAFANACTL_AGENT_MODE=false make all`.

---

## Orchestration

**Key rule:** Don't split work that touches the same files across agents.

| Phase | Agent Strategy | Rationale |
|-------|---------------|-----------|
| 1 (Pre-flight) | Main context | Interactive |
| 1.5 (Parity audit) | Main context | Requires gcx source reading + judgment |
| 2 (Core adapter) | Single agent or main | Many interdependent files |
| 3 (Schema) | Can delegate to agent | Isolated: `register.go` + `resource_adapter.go` |
| 4 (Commands) | Single agent for ALL | CRUD + ancillary share `commands.go`, `provider.go`, `client.go` |
| 5 (Smoke test) | Main context only | STOP gate — needs judgment + real API + user review |

**After agent phases:** Always run `GRAFANACTL_AGENT_MODE=false make lint` —
agents frequently trip `errchkjson`, `testpackage`, `nestif`, `gci`.

---

## Red Flags — STOP and Check

| Thought | Problem |
|---------|---------|
| "I'll just copy the gcx client as-is" | grafanactl uses different HTTP patterns — adapt, don't copy |
| "I'll skip the pre-flight, it's obvious" | K8s discovery may already cover this resource |
| "I know which commands to port" | Produce the parity audit table first — silent omissions cause incomplete ports |
| "The output format doesn't matter yet" | design-guide.md §1.3 compliance is mandatory, not polish — do it in Phase 4 |
| "I'll update the recipe later" | You won't. Update it NOW while friction is fresh |
| "This resource is too different for the pattern" | It's not — OnCall's 12 sub-resources fit. Ask for help if stuck |
| "I don't need tests, I verified manually" | httptest + round-trip tests are mandatory. `make all` enforces this |
| "I'll guess the endpoint name from the pattern" | Check gcx source. `SeverityService` != `SeveritiesService` |
| "I'll skip smoke tests, unit tests cover it" | Unit tests don't catch wrong endpoint names or wrapped request bodies |
| "omitempty works for this custom time type" | Use `omitzero` for struct-typed fields — Go 1.24+ requirement |

---

## Checklist

```
Prerequisites
[ ] gcx binary available (gcx --version)
[ ] Grafana context configured (grafanactl config view)
[ ] Provider directory created (/add-dir or manual)
[ ] Live API access verified (grafanactl resources schemas)

Phase 1: Pre-flight
[ ] Pre-flight questions answered (recipe section)
[ ] K8s discovery checked (skip if already there)
[ ] Recipe read front-to-back
[ ] gcx source files located (client, types, commands)

Phase 1.5: Feature parity audit
[ ] All gcx subcommands enumerated
[ ] Mapping table produced (Implemented / Deferred / N/A for each)
[ ] Deferred items have justification + target phase
[ ] Table reviewed before proceeding

Phase 2: Core adapter
[ ] types.go ported (json tags preserved, omitzero for struct fields)
[ ] client.go ported (rest.Config + http.Client pattern)
[ ] adapter.go wired (ToResource / FromResource)
[ ] resource_adapter.go wired (ResourceAdapter + Factory + init)
[ ] provider.go created (Provider interface)
[ ] Blank import in cmd/grafanactl/root/command.go
[ ] Tests written (client httptest + adapter round-trip)
[ ] GRAFANACTL_AGENT_MODE=false make all passes
[ ] SMOKE: resources get {alias} returns data
[ ] SMOKE: resources get {alias}/{id} matches gcx

Phase 3: Schema + example
[ ] Schema registered (static map, json.RawMessage)
[ ] Example registered (static map, json.RawMessage)
[ ] resources schemas {alias} -o json shows schema
[ ] resources examples {alias} prints YAML + JSON

Phase 4: Provider commands (format-compliant per design-guide.md §1.3)
[ ] CRUD: list (table + wide codecs), get, create -f, close
[ ] list default format is "text" (not "json")
[ ] wide codec registered with additional detail columns
[ ] json/yaml output wraps through ToResource (K8s envelope)
[ ] Ancillary commands wired
[ ] Endpoint names verified against gcx source
[ ] Table codecs exported for _test package
[ ] SMOKE: list IDs match gcx, get fields match, ancillary works
[ ] SMOKE: table/wide/json/yaml all render

Phase 5: Smoke test + finalize (STOP gate)
[ ] Structured jq diff run for list (IDs match)
[ ] Structured jq diff run for get (key fields match)
[ ] All four output formats verified (table/wide/json/yaml)
[ ] Comparison results pasted into conversation
[ ] Discrepancies justified (or fixed)
[ ] Recipe status tracker updated
[ ] Recipe gotchas updated with new discoveries
[ ] GRAFANACTL_AGENT_MODE=false make all (including doc regen)
[ ] All changes committed
```
