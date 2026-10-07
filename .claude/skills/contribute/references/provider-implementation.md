# Provider implementation

Use for an already-placed Cloud provider. Reuse accepted scope, placement and
command-contract decisions. The canonical recipe is
[provider-guide.md](../../../../docs/reference/provider-guide.md); consult
[provider discovery](../../../../docs/reference/provider-discovery-guide.md)
for unresolved API facts and the
[provider checklist](../../../../docs/design/provider-checklist.md) for review.

## Resolve implementation details


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


Record decisions in the existing issue, RFC or PR-scoped OpenSpec change under
[CONTRIBUTING](../../../../CONTRIBUTING.md#contribution-workflow). Novel architecture
decisions follow repository ADR conventions; routine choices matching precedent
need no separate document.

## Plan smoke verification


Prepare smoke test commands using test values for the implemented scope. Keep
them in the existing plan or working notes and report their results in the PR.

Cover only the verbs the stage actually implements — do not smoke-test CRUD
verbs the provider doesn't expose. Destructive commands use `--force`
(never `--yes`; see `docs/design/safety.md` §3.2). Mark in the plan which
commands mutate state and their intended test target so existing authorization
can be checked before running them.
Example pattern (replace with real product/resource names in actual spec):
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


## Implement


> **Guide**: `docs/reference/provider-guide.md` (Steps 1–7)
> **UX Guide**: `docs/design/`

Implement reviewable slices within the accepted scope and existing plan.

Follow `provider-guide.md` Steps 1–7, or the relevant subset for an extension.
Summary of the key steps:

1. Provider interface + `init()` with a single `providers.Register()` call + `providers.ConfigLoader` (mirror the SLO reference)
2. Config keys + validation
3. Commands with UX compliance
4. Types + capability-satisfying client; declare `adapter.Resource[T]` only for resources placed in the `resources` pipeline (returned from `TypedRegistrations()`, non-nil `Schema`)
5. Register via `adapter.NewProvider(...).WithCommands(...)` for adapter-backed resources (blank import in `cmd/gcx/root/command.go`; adapter registration flows through `TypedRegistrations()` — never call `adapter.Register()` directly)
6. Tests (interface compliance, client httptest request mapping; adapter round-trip only when an adapter exists)

**Key patterns** (see provider-guide.md for details):
- Hand-roll HTTP client (~200 LOC) — don't use generated OpenAPI clients
- Use `providers.ConfigLoader` (instantiate once in `Commands()`, `BindFlags` on the parent) — don't hand-roll config loading or import `cmd/gcx/config`
- Config key names use hyphen-case
- Adapter-backed resources must strip server-generated fields on Create/Update
- Declare adapter-backed resource types with `adapter.Resource[T]`; see the SLO reference in `internal/providers/slo/definitions/resource_adapter.go`.


## Verify


### Run Smoke Tests

Execute every command from [Plan smoke verification](#plan-smoke-verification) against a real Grafana
instance, using `bin/gcx` so you exercise the build under review.
Record results (pass/fail + output).

**Use the target and mutation scope the user authorized.** Read-only probes
can run within the selected target. For creates, updates and deletes, reuse
existing authorization for that target and scope; if missing, show the exact
commands and target before asking. This includes `delete <test-id> --force`: the
flag skips the CLI prompt rather than supplying authorization. Report unapproved
or unavailable smoke tests as **UNVERIFIED** with the reason.

If no instance or credentials are available, report every smoke test as
**UNVERIFIED** with that reason, and say what a reviewer must run before merge.
Do not block on it, and do not report untested commands as passing — an
`httptest`-proven client with UNVERIFIED smoke tests is an honest state; a silent
gap is not.

### Run Checklists

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

### Update Architecture Docs

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
| SLO | Same Grafana token | Plugin API | `internal/providers/slo/provider.go` — declarative `adapter.Resource[T]` reference |
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
