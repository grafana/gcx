# Wiring, Gates, and Preflight

Contents:
1. [Per-leaf wiring that cannot be skipped](#per-leaf-wiring-that-cannot-be-skipped)
   — including [the gap CI does not cover](#the-gap-ci-does-not-cover-and-it-is-a-judgement-call)
2. [What the root conformance suites do to your command](#what-the-root-conformance-suites-do-to-your-command)
3. [Preflight](#preflight)
4. [When CI fails](#when-ci-fails)
5. [Read the built contract the way agents will](#read-the-built-contract-the-way-agents-will)

## Per-leaf wiring that cannot be skipped

Adding a command to the tree is automatic (provider `Commands()`, datasource
registration, or a signal descriptor). The rest is not. Read the "Enforced by"
column literally — some rows are CI-enforced and some only look like it:

| Wiring | Where | Enforced by |
|--------|-------|-------------|
| Output protocol class | `cmd/gcx/root/testdata/output_classes.json` — one entry per new leaf | `TestConsistency_AllLeafCommandsHaveOutputClass` (also fails on stale entries after renames) |
| Token cost | `internal/agent/command_annotations.go`, or inline `cmd.Annotations` (signal specs use `CommandSpec.TokenCost`) | `TestConsistency_AllLeafCommandsHaveTokenCost` |
| LLM hint (medium/large costs) | same as token cost | `TestConsistency_NonSmallCommandsHaveLLMHint` |
| Cloud-only availability | `internal/agent/availability.go` (path prefix, covers subtrees) | **review only.** `TestConsistency_CloudOnlyPathsResolveToCommands` walks the declared paths and checks each resolves to a command — it catches a stale entry, not a missing one |
| Command→skill mapping | `internal/agent/command_skills.go` | **review only**, same direction: `TestConsistency_SkillMappingResolvesToCommands` validates declared keys, never requires one |
| Resource-type agent metadata, **native K8s types** | `internal/agent/known_resources.go` | `internal/agent/known_resources_test.go` |
| Resource-type agent metadata, **adapter-backed** | adapter `Registration.Operations` | **nothing.** `known_resources_test.go` only walks `agent.KnownResources`; no test asserts `Operations` is populated on an adapter registration. Review-enforced only |
| Generated reference docs | `GCX_AGENT_MODE=false mise run reference` | `mise run reference-drift` in CI |
| Package map | `docs/architecture/project-structure.md` | AGENTS.md PR checklist step 4 (human-enforced) |

### The gap CI does not cover, and it is a judgement call

A new datasource kind
is mounted as `datasources <kind>` automatically from
`datasources.AllProviders()`, but the generic auto-detecting `datasources query`
routes through two hand-maintained tables in
`cmd/gcx/datasources/query_routes.go`. Registration does not reach them, and no
test enforces parity — deliberately, because parity is not always correct:

- **The generic `<uid> <expr>` contract fits your kind** → add a `dispatch`
  entry plus a small handler. Otherwise a caller who reasonably reaches for
  `datasources query` gets the bare "datasource type %q is not supported"
  default.
- **It does not fit** → add a `redirects` entry instead, built with
  `structuredQueryRedirect`. CloudWatch is the worked example: its query is
  structured (namespace, metric, dimensions, region, statistic, period), no
  single `expr` can carry it, so the command returns an error naming the typed
  command to use. That is the honest outcome, not a degraded generic path.

  **The durable requirement is ordering: a redirect has to run before the
  expression is resolved.** Otherwise `gcx datasources query <uid>` with no
  expression fails on "expression required" before it can name your typed
  command — losing the redirect in exactly the invocation that needs it.

  Routing is declarative now, so you get that ordering for free: the redirect
  lookup in `(*genericQueryOpts).run` sits ahead of both `ResolveExpr` and the
  dispatch lookup, and the characterization suite pins it. You choose a table,
  not a position in a function. The two tables must stay disjoint and keyed by
  normalized kinds (`NormalizeKind`); `query_routes_internal_test.go` enforces
  both, so "a case *and* a guard" is not constructible.

The unsupported-kind message is derived from the tables, so you never edit that
string — but two tests pin its exact value, because it is user-visible text on a
GA path: `TestQueryRoutesSupportedKindsIsTheSortedUnion` and
`wantUnsupportedMessage` in `query_unsupported_test.go`. Both fail with the old
and new lists side by side, so the update is mechanical.

To see which kinds are currently outside the tables, derive it rather than
trusting a list here — it changes with every datasource PR:

```bash
rg -n 'RegisterProvider' internal/datasources/providers  # registered kinds
rg -n '^\s+"' cmd/gcx/datasources/query_routes.go       # routed kinds, both tables
```

Read `newQueryRoutes()` directly rather than trusting the second pattern; it is a
starting point, not a parser. Match registration on the call rather than listing
the directory — that package also holds non-kind files, so a file listing
overstates the set.

The difference is the candidate set, minus the kinds excluded by design (a
structured query has no single-`expr` representation, so it gets a redirect).
A typed `query` leaf is not always in a `query.go` — tempo builds its own in
`internal/datasources/tempo/search.go` — so confirm the leaf exists before
treating a kind as a candidate.

## What the root conformance suites do to your command

| Suite | What it proves — and the common failure |
|-------|------------------------------------------|
| `TestAgentConformance_EveryFiniteLeafEmitsOneJSONValue` | Runs every finite/artifact/stream leaf with NO arguments in a fully isolated environment (empty HOME, closed stdin, 20s timeout, agent mode on). Failures: cobra usage text on stdout instead of one JSON error document; a prompt or editor that blocks (interactive paths must not block in agent mode); in-band `exitCode` disagreeing with the process exit code. |
| `TestConsistency_*` family | The wiring table above. The error text names the file to edit. |
| `TestSkillsGcxInvocationsMatchCommandTree` | Every gcx invocation in skill markdown — shell fences and inline backtick spans, `gcx` and `bin/gcx` forms — must resolve against the real tree. Covers **both** trees: the embedded `claude-plugin/skills/` bundle and repo-local `.claude/skills/`. Each tree must yield discovered documents, a `SKILL.md` per skill root, and at least one invocation — there is no invocation-count floor. |

These enforce wiring and protocol shape only. Request mapping, filtering
semantics, pagination and error taxonomy are proven by YOUR package tests
(self-review.md T8).

## Preflight

Format what you actually edited — including changes you have not committed yet,
which a base-diff would miss — then gate:

```bash
mise exec -- gofmt -w <the .go files you edited>
mise run gate
GCX_AGENT_MODE=false mise run all
```

`go` and `gofmt` come from mise and may not be on `PATH`; prefix them with
`mise exec --` (or use a `mise run` task) so you get the pinned version.

`mise run gate` is the fast inner loop (lint + tests + build).
`GCX_AGENT_MODE=false mise run all` is the pre-push gate and already subsumes
validate-skills, lint, tests, build and docs — you do not need to run those
separately as well.

For a skill-only change — both gates cover the embedded `claude-plugin/skills/`
bundle **and** repo-local `.claude/skills/`, so either tree is checked:

```bash
mise run validate-skills
mise exec -- go test ./cmd/gcx/root/ \
  -run 'TestSkillsGcxInvocationsMatchCommandTree|TestSkillDocsFromFS|TestExtractInvocations|TestValidateInvocation'
```

All four matter and they fail for different reasons: the first validates your
invocations against the real tree, `TestSkillDocsFromFS` proves the walk still
reaches nested `references/` markdown, and the last two pin extraction and
validation behaviour so you can tell an extractor bug from genuine drift.

Notes:

- `GCX_AGENT_MODE=false` matters: agent-mode auto-detection flips output
  defaults and silently produces wrong generated docs from agent environments.
- A resolved base is for scoping a *review* diff (self-review.md T10), not for
  deciding what to format.
- If a tool is unavailable locally (mkdocs for the docs build, for instance),
  report that gate as SKIPPED with the reason — never as green. CI covers it.
- The authoritative checklists live in AGENTS.md (Mandatory Pre-Commit /
  Pull Request). This is the command form, not a replacement.

## When CI fails

| Failure | Meaning | Fix |
|---|---|---|
| `TestConsistency_AllLeafCommandsHaveOutputClass` | New leaf missing from the class fixture, or a stale entry after a rename | Add or update the entry in `cmd/gcx/root/testdata/output_classes.json` |
| `TestConsistency_AllLeafCommandsHaveTokenCost` | `token_cost` missing, **or** present with an invalid value — it must be a bare `small`/`medium`/`large`, or the qualified `<level> (<qualifier>)` form | Add or correct the value: registry entry in `internal/agent/command_annotations.go`, or inline `cmd.Annotations`. Read the error text — "missing" and "invalid token cost %q" are different failures |
| `TestConsistency_NonSmallCommandsHaveLLMHint` | A bare `medium`/`large` cost with no `llm_hint` | Add the hint alongside the cost, same place |
| `TestAgentConformance_EveryFiniteLeafEmitsOneJSONValue` fails or times out | Usage text on stdout instead of one JSON document, or a prompt/editor survives agent mode | Return the in-band error document; never block in agent mode |
| `TestSkillsGcxInvocationsMatchCommandTree` | A skill edit in either tree references an unknown command or flag | Fix the invocation, or use a non-shell fence / a `<placeholder>` for hypotheticals |
| `mise run validate-skills` | Skill front matter unparseable, or missing `name` / `description`. A `description:` value containing a colon must be quoted or a `>-` block — an unquoted plain scalar with `Foo: bar` in it is invalid YAML | Fix the front matter |
| `mise run reference-drift` (CI) | Generated docs not regenerated | `GCX_AGENT_MODE=false mise run reference`, commit the diff |
| `mise run lint` reports findings in other worktrees | Stale golangci-lint cache | `mise exec -- golangci-lint cache clean`, re-run |

## Read the built contract the way agents will

```bash
mise run build
bin/gcx commands --flat -o json
bin/gcx help-tree
```

Check your leaf's entry: description, args, flags, token cost, hint. This is the
exact metadata agents route on — if it reads ambiguous next to its nearest
sibling, fix it now (self-review.md T6).
