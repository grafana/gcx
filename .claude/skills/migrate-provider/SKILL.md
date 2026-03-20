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

## Workflow

```
Phase 1: Pre-flight           → answer 6 questions, locate gcx source
Phase 2: Core adapter          → types, client, adapter, resource_adapter, provider
Phase 3: Schema + example      → register in adapter.Registration
Phase 4: Provider commands     → CRUD redirects + ancillary subcommands
Phase 5: Smoke test + recipe   → side-by-side gcx comparison, update recipe
```

Phases are **sequential**. See Orchestration section for within-phase parallelism.

### Phase 1: Pre-flight

1. Read recipe front to back
2. Run pre-flight checklist (recipe section) — answer all 6 questions
3. Locate gcx source: `pkg/grafana/{resource}/`, `cmd/`
4. Check K8s discovery: `grafanactl resources schemas | grep -i {resource}`
   — if already discovered, no provider needed

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

**After Phase 4:** smoke test ALL commands side-by-side with gcx.

### Phase 5: Smoke Test + Update Recipe

Run recipe Step 8 (side-by-side comparison) for every command. Then update
recipe status tracker and gotchas. Final: `GRAFANACTL_AGENT_MODE=false make all`.

---

## Orchestration

**Key rule:** Don't split work that touches the same files across agents.

| Phase | Agent Strategy | Rationale |
|-------|---------------|-----------|
| 1 (Pre-flight) | Main context | Interactive |
| 2 (Core adapter) | Single agent or main | Many interdependent files |
| 3 (Schema) | Can delegate to agent | Isolated: `register.go` + `resource_adapter.go` |
| 4 (Commands) | Single agent for ALL | CRUD + ancillary share `commands.go`, `provider.go`, `client.go` |
| 5 (Smoke test) | Main context only | Needs judgment + real API |

**After agent phases:** Always run `GRAFANACTL_AGENT_MODE=false make lint` —
agents frequently trip `errchkjson`, `testpackage`, `nestif`, `gci`.

---

## Red Flags — STOP and Check

| Thought | Problem |
|---------|---------|
| "I'll just copy the gcx client as-is" | grafanactl uses different HTTP patterns — adapt, don't copy |
| "I'll skip the pre-flight, it's obvious" | K8s discovery may already cover this resource |
| "I'll update the recipe later" | You won't. Update it NOW while friction is fresh |
| "This resource is too different for the pattern" | It's not — OnCall's 12 sub-resources fit. Ask for help if stuck |
| "I don't need tests, I verified manually" | httptest + round-trip tests are mandatory. `make all` enforces this |
| "I'll guess the endpoint name from the pattern" | Check gcx source. `SeverityService` != `SeveritiesService` |
| "I'll skip smoke tests, unit tests cover it" | Unit tests don't catch wrong endpoint names or wrapped request bodies |
| "omitempty works for this custom time type" | Use `omitzero` for struct-typed fields — Go 1.24+ requirement |

---

## Checklist

```
Phase 1: Pre-flight
[ ] Pre-flight questions answered (recipe section)
[ ] K8s discovery checked (skip if already there)
[ ] Recipe read front-to-back
[ ] gcx source files located (client, types, commands)

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

Phase 4: Provider commands
[ ] CRUD: list (table codec + json/yaml), get, create -f, close
[ ] Ancillary commands wired
[ ] Endpoint names verified against gcx source
[ ] Table codecs exported for _test package
[ ] SMOKE: list IDs match gcx, get fields match, ancillary works
[ ] SMOKE: table/wide/json/yaml all render

Phase 5: Finalize
[ ] GRAFANACTL_AGENT_MODE=false make all (including doc regen)
[ ] Recipe status tracker + gotchas updated
[ ] All changes committed
```
