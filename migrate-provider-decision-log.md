# /migrate-provider Skill Decision Log

Decisions made during the adaptive provider migration that should feed back
into the skill recipe.

---

## D-001: Compliance docs incorporated into Phase 1 design

**Date:** 2026-03-27
**Context:** Starting adaptive telemetry provider migration, Phase 1 (CLI UX + resource adapter design).

The skill recipe references `gcx-provider-recipe.md` as the mechanical source of truth but
doesn't explicitly list which project docs must be checked for compliance during design.

**Decision:** Phase 1 design must be validated against:

1. **CONSTITUTION.md** — Architecture invariants, CLI grammar (`$AREA $NOUN $VERB`), provider
   architecture rules (dual CRUD paths, sub-resource nesting, TypedCRUD requirement,
   ResourceIdentity, Schema/Example on Registration, ExternalHTTPClient for non-Grafana APIs).
2. **docs/reference/design-guide.md** — UX requirements: output codecs (text/wide/json/yaml),
   exit codes, error design, agent mode, pipe awareness, ConfigLoader pattern, K8s manifest
   wrapping rules, help text standards.
3. **docs/reference/provider-guide.md** — Provider interface contract (Name, ShortDesc,
   Commands, Validate, ConfigKeys), registration via `init()`, config key declaration,
   env var pattern (`GRAFANA_PROVIDER_{NAME}_{KEY}`).
4. **docs/reference/provider-discovery-guide.md** — Decision framework (auth strategy,
   API client type, envelope mapping, command surface, package layout, staging).

**Action for skill:** Add a "Compliance Checklist" section to Stage 1 (Audit) that
requires the lead to explicitly check each doc and record which rules apply. Currently
the skill assumes the builder knows the project conventions — this should be explicit.

---

## D-002: Progressive design discovery replaces linear audit

**Date:** 2026-03-27
**Context:** User requested brainstorming-style back-and-forth instead of producing
all three audit artifacts before getting feedback.

**Decision:** For Phase 1, replace the skill's "produce three artifacts then gate" flow
with progressive discovery stages:

1. **Stage 1A — CLI UX design** — Command tree, naming, grammar compliance. Get user
   approval before proceeding.
2. **Stage 1B — Resource adapter design** — GVK mapping, TypedCRUD setup, schema/example
   registration. Get user approval.
3. **Stage 1C — Config & auth design** — ConfigKeys, ConfigLoader usage, env vars,
   GCOM stack lookup. Get user approval.

Each stage produces a focused design proposal, gets feedback, and iterates before
the next stage begins. This catches design misalignment early instead of at the
end of a monolithic audit.

**Action for skill:** Consider adding a "progressive discovery" variant to the Audit
stage for complex providers where the user wants iterative alignment. The current
three-artifact gate is still valid for straightforward ports where the mapping is obvious.

---

## D-003: Migration must produce docs harness artifacts

**Date:** 2026-03-27
**Context:** User requires compliance with the project's documentation harness.

**Decision:** Each migration must produce these artifacts in order:

1. **ADR(s)** — Architecture Decision Record(s) for non-obvious design choices
   (e.g., "one provider with three subareas" vs "three providers"). Written during
   Phase 1 design discovery, before implementation begins. Lives in `docs/decisions/`.
2. **Spec(s)** — Formal spec documents (spec.md + plan.md + tasks.md) for each
   implementation stage. Written after design is approved, before code.
3. **Research docs** (optional) — If API discovery or source code analysis was
   needed, capture findings in `docs/research/` for future reference.
4. **Architecture docs update** — After implementation is complete and smoke tests
   pass, update `docs/architecture/` to reflect the new provider. This is the
   *last* step, not done during implementation.

**Action for skill:** Add a "Documentation Artifacts" section to the pipeline overview
that makes these deliverables explicit. Currently the skill only produces code artifacts
and a comparison report — no persistent design documentation.

---

## D-004: Phase 0 completed — design discovery + ADR

**Date:** 2026-03-27
**Context:** Reflecting on what we actually did vs what the skill prescribes.

### What happened

The skill defines three stages: Audit → Build → Verify. We inserted a new
**Phase 0: Design Discovery** before Audit that the skill doesn't have:

```
Phase 0: Design Discovery (NEW)
  ├── Read gcx source (3 clients, 3 command files, examples)
  ├── Read gcx reference docs (CONSTITUTION, design-guide, provider-guide, discovery-guide)
  ├── Read existing provider patterns (SLO as reference)
  ├── Progressive brainstorming with user (3 rounds):
  │   ├── 1A: CLI UX — command tree, grammar compliance
  │   ├── 1B: Adapter scope — which resources get adapters (rejected read-only adapters)
  │   └── 1C: Verb naming — `show` for provider-only collections
  └── ADR written and approved → docs/adrs/adaptive-provider/001-*.md
```

Key decisions made:
- One provider, three subareas (`gcx adaptive metrics|logs|traces`)
- 2 adapters (exemptions + policies), rest provider-only
- `show` verb for provider-only collections (not `list`)
- Auth via `LoadCloudConfig()` + `ExternalHTTPClient()`, no provider-specific ConfigKeys
- Dropped `insights` and `tenants` (untyped internal tooling)
- Bare domain-type JSON for provider-only resources (preserves pipe workflows)

### What's next

Phase 1 should produce the **parity table**, **architectural mapping**, and
**verification plan** — the three artifacts from the skill's Stage 1 (Audit).
These now serve as the spec/plan for Build and Verify, grounded in the
approved ADR. The parity table is partially done (we mapped all gcx commands
during brainstorming) but needs to be formalized.

### Action for skill

The current skill assumes you jump straight into auditing gcx source and
producing artifacts. For complex or novel providers, a design discovery phase
with human-in-the-loop brainstorming produces better results:

1. **Add Phase 0 (Design Discovery)** as an optional stage before Audit.
   Triggered when the provider has non-obvious design choices (multiple
   resource types, mixed CRUD/action patterns, shared auth across subareas).
2. **Phase 0 output is an ADR**, not the three audit artifacts. The ADR
   captures the "why" behind structural choices that the parity table and
   arch mapping take as given.
3. **Phase 0 gate**: ADR approved by user before proceeding to Audit.
4. The existing Audit stage then becomes Phase 1, grounded in the ADR's
   decisions rather than discovering them during artifact production.

---

## D-005: Replace skill audit artifacts with /plan-spec

**Date:** 2026-03-27
**Context:** The skill defines three audit artifacts (parity table, architectural
mapping, verification plan) as sealed envelopes for Build and Verify. The project
also has a docs harness with `/plan-spec` producing spec.md + plan.md + tasks.md.

**Decision:** Replace the skill's three audit artifacts with a single `/plan-spec`
invocation. The mapping is:

| Skill artifact | Spec equivalent |
|---|---|
| Parity table | `spec.md` acceptance criteria (one per command) |
| Architectural mapping | `plan.md` approach (pattern translations, package layout) |
| Verification plan | `plan.md` test strategy + `tasks.md` verification tasks |

The spec format is already standardized, reusable by `/build-spec`, and
understood by the rest of the toolchain. The skill's custom artifact format
is redundant.

**Action for skill:** Replace the skill's three custom audit artifacts (parity
table, architectural mapping, verification plan) with the spec document
structure (spec.md + plan.md + tasks.md). The mapping is clean:

- **Parity table** → `spec.md` acceptance criteria
- **Architectural mapping** → `plan.md` approach section
- **Verification plan** → `plan.md` test strategy + `tasks.md` verification tasks

The spec format is the project's standard regardless of whether `/plan-spec`
and `/build-spec` skills are installed. When those skills are available, use
them to automate spec generation and build execution. When absent, the skill
produces the same spec files directly — the format is a document convention,
not a tool dependency.

The revised pipeline:

```
Phase 0: Design Discovery → ADR (approved)
Phase 1: Spec
  ├── Produce spec.md + plan.md + tasks.md (via /plan-spec or manually)
  └── User approves
Phase 2: Build
  ├── Execute tasks.md (via /build-spec or skill's agent team)
  └── make all passes
Phase 3: Verify → smoke tests, comparison report
Phase 4: Docs update → architecture docs
```

---

## D-006: Drop sealed envelope isolation — spec structure is sufficient

**Date:** 2026-03-27
**Context:** The skill enforces strict isolation between Build and Verify via
"sealed envelopes" — Build receives only the parity table + arch mapping,
Verify receives only the verification plan. Neither may see the other's
content. This was designed to prevent overfitting (builders gaming tests)
and ensure black-box verification.

**Decision:** Drop the sealed envelope concept. The spec format provides
natural separation without artificial barriers:

| Role | Reads | Doesn't need |
|---|---|---|
| Builder | spec.md (what) + plan.md (how) + implementation tasks | Smoke test commands, expected output comparisons |
| Verifier | spec.md (what) + verification tasks | Internal function names, package structure choices |

**Why envelopes don't add value:**

1. **Acceptance criteria are the shared contract.** Both builder and verifier
   need to see them. A builder writing unit tests from acceptance criteria is
   good engineering, not overfitting.
2. **The overfitting concern assumes adversarial agents.** An agent team
   working from the same spec has no incentive to game verification.
3. **Envelope enforcement is fragile.** The skill uses spawn prompts to
   control what each teammate sees, but any agent can read files on disk.
   The isolation is theatrical, not real.
4. **Spec format naturally separates concerns.** Implementation details live
   in plan.md and implementation tasks. Verification expectations live in
   spec.md acceptance criteria and verification tasks. No artificial barrier
   needed.

**What to keep:** Verification tasks in `tasks.md` should describe behavioral
expectations ("list returns JSON array matching gcx output"), not
implementation details ("check that client.ListPolicies calls the right URL").
This is just good acceptance criteria writing, not envelope isolation.

**Action for skill:** Remove the Build envelope / Verify envelope sections,
the spawn prompt templates that enforce isolation, and the "red flags" about
peeking across envelopes. Replace with guidance that verification tasks
should be written as behavioral expectations against the spec's acceptance
criteria.

---

## D-007: Revised skill pipeline (consolidated learnings)

**Date:** 2026-03-27
**Context:** After completing Phase 0 (design) and Phase 1 (spec planning) for
the adaptive provider, we have enough signal to propose a revised skill pipeline
that incorporates all decisions D-001 through D-006.

### Proposed pipeline

```
Phase 0: Requirements Gathering & Context (autonomous)
  ├── Read grafana-cloud-cli source for the target provider
  ├── Read gcx reference docs (CONSTITUTION, design-guide, provider-guide, discovery-guide)
  ├── Read existing provider patterns (closest match — e.g., fleet for cloud APIs, SLO for plugin APIs)
  ├── Map every gcx subcommand → proposed gcx equivalent (parity table as working doc)
  └── Output: context bundle (source summary, parity mapping, compliance notes)

Phase 1: Design Discovery (interactive brainstorming → ADR)
  Progressive disclosure, each stage gets user approval before next:
  ├── 1A: CLI UX — command tree, naming, grammar compliance (CONSTITUTION §CLI Grammar)
  ├── 1B: Resource adapters — which resources get adapters, GVK mapping, verb choice
  │   └── Key question: does adapter envelope break pipe workflows? (show → apply)
  ├── 1C: Auth & config — ConfigKeys, ConfigLoader, env vars, GCOM/instance lookup
  ├── 1D: Architecture — package layout, client construction, shared helpers
  └── Output: ADR documenting all decisions + DESIGN.md updated

Phase 2: Spec Planning (/plan-spec or manual)
  ├── spec.md — FRs, ACs (Given/When/Then), NCs, risks
  ├── plan.md — architecture, design decisions, HTTP client reference, compatibility
  ├── tasks.md — dependency graph, waves, per-task deliverables + ACs
  └── Output: approved spec package

Phase 3: Build (/build-spec or skill's agent team)
  ├── Execute tasks.md waves in order
  ├── make lint checkpoint between each task
  ├── make all gate before declaring build complete
  └── Output: implementation code

Phase 4: Verification
  ├── 4A: Spec compliance — verify each AC from spec.md against built code
  ├── 4B: Smoke tests — run provider commands against live instance, compare to gcx output
  ├── 4C: Recipe update — add provider to status tracker, record any gotchas
  └── Output: comparison report + recipe updates

Phase 5: Docs Update (only after verification passes)
  └── Update docs/architecture/ to reflect new provider
```

### Key improvements over current skill

1. **Phase 0 is autonomous.** The current skill starts with "read gcx source"
   as part of the Audit stage interleaved with artifact production. Separating
   context gathering makes it reusable (the context bundle can feed any
   downstream phase).

2. **Phase 1 uses progressive disclosure.** Instead of producing all artifacts
   at once, each design aspect gets its own brainstorming round with user
   feedback. This caught the adapter/verb issue (read-only adapters break pipe
   workflows) early, before it would have been baked into a monolithic audit.

3. **No sealed envelopes.** The spec format provides natural separation between
   build and verify concerns (D-006).

4. **Spec format replaces custom artifacts.** Parity table → spec ACs,
   architectural mapping → plan, verification plan → tasks (D-005).

5. **HTTP client reference in plan.md, templated in recipe.** The plan now
   includes concrete endpoint tables, auth helper signatures, and client
   construction patterns. The recipe should include a checklist/template
   requiring this section ("your plan.md MUST include: endpoint table with
   method/path/purpose/notes, auth helper signature, client construction
   pattern"). The content is per-provider; the structure is generic and
   should be prompted for — without it, the plan shipped without this
   section until the user asked.

6. **CONSTITUTION compliance is a first-class check in Phase 1.** The current
   skill doesn't explicitly require checking CONSTITUTION.md during design.
   We caught the verb mimicry rule because we read it; the skill should
   mandate it (D-001).

### What to keep from the current skill

- **Red flags table** — the "stop and check" patterns are still valuable
  (e.g., copying gcx client verbatim, skipping parity audit, guessing
  endpoints). Keep these but adapt to the new phase structure.
- **File ownership table** — useful for agent team parallelism in Phase 3.
- **Recipe update requirement** — Phase 4C should still update the recipe
  status tracker and gotchas section.

---

## D-008: Import cycle requires shared auth subpackage

**Date:** 2026-03-27
**Context:** During Phase 3 (Build), T5 wired `Commands()` and
`TypedRegistrations()` in the parent `adaptive` package, which imports the
signal subpackages (`metrics`, `logs`, `traces`). But those subpackages
imported the parent for `ResolveSignalAuth`. Result: import cycle.

**Decision:** Move shared auth helpers to a dedicated subpackage
(`internal/providers/adaptive/auth/`) that has no upward dependency on the
parent. Signal subpackages import `auth`, parent imports signal subpackages.
No cycle.

**Action for skill:** The spec/plan should mandate a `{provider}/auth/`
subpackage for the shared auth helper whenever the provider uses subpackages
per signal/area. The current plan template puts `auth.go` in the parent
package, which works for single-package providers (fleet) but breaks when
subpackages import the parent and the parent imports subpackages. Add this
as a structural rule in the package layout section:

```
internal/providers/{name}/
├── auth/           ← shared auth (NO upward imports)
│   └── auth.go
├── {signal_a}/     ← imports auth/, NOT parent
│   ├── client.go
│   └── commands.go
├── {signal_b}/
│   └── ...
└── provider.go     ← imports signal subpackages
```

---

## D-009: Variable shadowing with `auth` package name

**Date:** 2026-03-27
**Context:** After moving auth to its own package, the subpackages import it
as `auth`. Every call site used `auth, err := auth.ResolveSignalAuth(...)`
which shadows the package name with a local variable.

**Decision:** Use `signalAuth` as the local variable name everywhere.
Resource adapter files that also have an `auth` import use the alias
`adaptiveauth` to avoid the collision entirely.

**Action for skill:** Code templates in the skill should use `signalAuth`
as the conventional variable name for the resolved auth tuple. The spec
template's HTTP client reference section should show:

```go
signalAuth, err := auth.ResolveSignalAuth(ctx, h.loader, "{signal}")
client := NewClient(signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient)
```

---

## D-010: Lint compliance checklist for new providers

**Date:** 2026-03-27
**Context:** The build produced 18 lint issues on first pass. Most were
predictable from existing provider patterns but weren't documented.

**Decision:** Codify the following as a mandatory lint checklist for the
Build phase:

| Rule | Pattern | Fix |
|------|---------|-----|
| `recvcheck` | Mixed value/pointer receivers on ResourceIdentity types | `//nolint:recvcheck` on the type decl (matches fleet) |
| `gochecknoglobals` | Static descriptor vars | `//nolint:gochecknoglobals` on the var decl |
| `testpackage` | Test files in same package | Use `_test` suffix package + import the package under test |
| `gci` | Import ordering | stdlib → external → internal, blank lines between groups |
| `embeddedstructfieldcheck` | Embedded struct in opts | Blank line between embedded field and regular fields |
| `testifylint/go-require` | `require` in HTTP handler goroutines | Use `assert` or `if err != nil { t.Fatal(...) }` |
| `staticcheck/S1016` | Struct literal when types are convertible | Use type conversion: `MetricRule(recommendation)` |
| `nestif` | Deeply nested if blocks | Extract helpers (e.g., `getCachedSignalAuth`) |
| `nonamedreturns` | Named return values | Remove named returns, use explicit vars |
| `dupl` | Duplicate test functions for similar endpoints | `//nolint:dupl` on the test or extract helper |

**Action for skill:** Add this table as a "Lint Compliance Checklist" in
the Build phase. Builders should run `make lint | grep {provider}` after
each task and fix before merging. The current skill has no lint gate between
tasks — add one.

---

## D-011: Parallel wave execution is effective but needs post-merge fixup

**Date:** 2026-03-27
**Context:** Wave 2 ran T2, T3, T4 in parallel via 3 Sonnet agents. All
completed independently. However, T5 (integration wiring) required
fixing the import cycle (D-008) and variable shadowing (D-009) that none
of the parallel agents anticipated because each worked in isolation.

**Decision:** This is acceptable overhead — the fixup in T5 took ~5 minutes
vs. the ~10 minutes saved by parallel execution. The key insight is that
**T5 is not just wiring — it's also the integration fixup task**. Its scope
should explicitly include:

1. Wire Commands() and TypedRegistrations()
2. Add blank import in `cmd/gcx/root/command.go`
3. Fix import cycles introduced by subpackage → parent references
4. Fix variable name collisions from package aliasing
5. Run lint and fix all new issues

**Action for skill:** T5 (or equivalent integration task) should include
explicit steps for import cycle resolution and lint fixup. Currently the
task description says "Wire Commands() and TypedRegistrations(), update
provider_test, run make all" — too narrow. Add import cycle and lint as
explicit deliverables.

---

## D-012: Smoke testing caught three classes of agent-introduced bugs

**Date:** 2026-03-27
**Context:** After unit tests passed and lint was clean, smoke testing against
a live Grafana Cloud stack (`--context=dev`) revealed three bug classes that
unit tests with mocked HTTP servers did not catch.

### Bug 1: Response envelope hallucination

The metrics client wrapped JSON deserialization in `struct { Rules []MetricRule
"json:\"rules\"" }` but the real API returns a bare `[]MetricRule`. The agent
assumed a wrapper object because many APIs use them — but the grafana-cloud-cli
source clearly shows `json.Unmarshal(resp.Body, &rules)` into a bare slice.

The same hallucination applied to `ListRecommendations` (wrapped in
`{"recommendations": [...]}`) and `SyncRules` POST body (wrapped in
`{"rules": [...]}`).

**Root cause:** The agent was given type definitions but not the exact
deserialization code from the source. It inferred a wrapper pattern.

### Bug 2: Auth method substitution (Bearer for Basic)

The logs and traces HTTP clients used `req.Header.Set("Authorization",
"Bearer "+c.apiToken)` instead of `req.SetBasicAuth(tenantID, apiToken)`.
The metrics client was correct. The agents implementing T3 and T4 defaulted
to the more common Bearer pattern despite the spec and source explicitly
showing Basic auth with `{instanceID}:{apToken}`.

**Root cause:** Bearer auth is the more common HTTP auth pattern in Go
codebases. The agent defaulted to it despite explicit instructions.

### Bug 3: Missing wide codec

The logs `patterns show` command registered only a `table` codec but not
`wide`. Running `-o wide` produced "unknown output format 'wide'". The spec
(FR-017) requires all four formats.

**Root cause:** The agent prompt specified "table/wide codecs" but the
implementation only created one codec struct without a `wide` variant.

### Action for skill

**Add mandatory smoke test gate before verification is considered complete.**
The current pipeline runs `make all` (unit tests + lint) but not live API
calls. Unit tests with mocked servers cannot catch:

1. **Wire format mismatches** — mocked servers echo whatever shape the test
   author assumes, which may differ from the real API.
2. **Auth method errors** — mocked servers don't validate auth headers.
3. **Missing codec registrations** — only caught when the command actually runs.

The revised verification phase should be:

```
Phase 4: Verification
  ├── 4A: make all (unit tests + lint + build)
  ├── 4B: Smoke tests — MANDATORY for each show command:
  │   ├── -o json   (verify deserialization works)
  │   ├── -o table  (verify table codec registered)
  │   ├── -o wide   (verify wide codec registered)
  │   └── -o yaml   (verify yaml round-trip)
  ├── 4C: Adapter smoke — MANDATORY for each TypedCRUD:
  │   ├── resources schemas (verify registration visible)
  │   └── resources get (verify envelope + deserialization)
  └── 4D: Spec compliance + comparison report
```

**Also add to builder prompts:** "Do NOT infer response envelope shapes.
Copy deserialization code verbatim from the grafana-cloud-cli source. If
the source does `json.Unmarshal(body, &slice)`, the new client must do the
same — never wrap in a struct unless the source does."

---

## D-013: Provider HTTP clients need debug logging for live troubleshooting

**Date:** 2026-03-27
**Context:** During smoke testing, failed API calls produced only
`"Unexpected error"` in agent mode. The actual HTTP status and error body
were buried in the error chain but invisible without `-vvv`. Even with
`-vvv`, the provider clients emit no `slog.Debug` calls for
request/response details.

**Decision:** File as follow-up work. Each HTTP client's `doRequest` method
should log:

```
slog.Debug("adaptive: HTTP request",
    "signal", signal,
    "method", method,
    "url", url,
    "status", resp.StatusCode,
)
```

At `-vvv` (debug level), this gives operators enough to diagnose auth
failures, wrong URLs, and unexpected status codes without reading source.

**Action for skill:** Add a "Debug Logging" section to the HTTP client
template in the build phase. Every `doRequest` helper should include
`slog.Debug` for method, URL, and response status. This is a cross-cutting
concern that should be in the template, not left to individual builders.

Also: consider capturing a logging invariant in CONSTITUTION.md and the
design guide — "provider HTTP clients MUST log request method, URL, and
response status at debug level."

---

## Builder Handoff Notes

Context the builder needs that isn't in the spec/plan:

### Source code locations (grafana-cloud-cli)

```
/Users/igor/Code/grafana/grafana-cloud-cli/
├── pkg/grafana/adaptive_metrics/client.go   # Types + API client
├── pkg/grafana/adaptive_logs/client.go      # Types + API client
├── pkg/grafana/adaptive_traces/client.go    # Types + API client
├── cmd/observability/adaptive_metrics.go    # CLI commands
├── cmd/observability/adaptive_logs.go       # CLI commands
└── cmd/observability/adaptive_traces.go     # CLI commands
```

The user has added `/Users/igor/Code/grafana/grafana-cloud-cli` as a working
directory — use it directly, don't clone.

### Closest existing pattern

`internal/providers/fleet/` — uses `LoadCloudConfig()` + Basic auth +
`ExternalHTTPClient()` + config caching via `SaveProviderConfig()`. Read
`fleet/provider.go` and `fleet/client.go` before starting T1.

### Gotchas discovered during design

1. **Logs exemptions response envelope**: `ListExemptions` returns
   `{"result": [...]}` — all other endpoints return bare arrays.
2. **Metrics rules ETag**: `GetRules` returns ETag in response header;
   `ApplyRules` must send `If-Match` header. If ETag is empty string,
   still set the header (server may accept empty for first write).
3. **Logs patterns full-array semantics**: `ApplyRecommendations` POSTs
   the ENTIRE array, not just modified entries. The client must GET all,
   modify matched entries, POST all back.
4. **Policy.Body is `map[string]any`**: Traces policies have a flexible
   `body` field that varies by policy type. Use `json:",omitzero"` not
   `json:",omitempty"` (per gcx conventions for Go 1.24+).
