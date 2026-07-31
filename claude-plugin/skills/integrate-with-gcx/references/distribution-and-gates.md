# Wiring, Gates, and Preflight

## Per-leaf wiring that cannot be skipped

Adding a command to the tree is automatic (provider `Commands()`, datasource
registration, or a signal descriptor). These are not, and CI fails without them:

| Wiring | Where | Enforced by |
|--------|-------|-------------|
| Output protocol class | `cmd/gcx/root/testdata/output_classes.json` — one entry per new leaf | `TestConsistency_AllLeafCommandsHaveOutputClass` (also fails on stale entries after renames) |
| Token cost | `internal/agent/command_annotations.go`, or inline `cmd.Annotations` (signal specs use `CommandSpec.TokenCost`) | `TestConsistency_AllLeafCommandsHaveTokenCost` |
| LLM hint (medium/large costs) | same as token cost | `TestConsistency_NonSmallCommandsHaveLLMHint` |
| Cloud-only availability | `internal/agent/availability.go` (path prefix, covers subtrees) | `TestConsistency_CloudOnlyPathsResolveToCommands` |
| Resource-type agent metadata, **native K8s types** | `internal/agent/known_resources.go` | `internal/agent/known_resources_test.go` |
| Resource-type agent metadata, **adapter-backed** | adapter `Registration.Operations` | **nothing.** `known_resources_test.go` only walks `agent.KnownResources`; no test asserts `Operations` is populated on an adapter registration. Review-enforced only |
| Generated reference docs | `GCX_AGENT_MODE=false mise run reference` | `mise run reference-drift` in CI |
| Package map | `docs/architecture/project-structure.md` | AGENTS.md PR checklist step 4 (human-enforced) |

**The gap CI does not cover:** a new datasource kind is mounted as
`datasources <kind>` automatically from `datasources.AllProviders()`, but the
generic auto-detecting `datasources query` dispatches through a hand-maintained
switch in `cmd/gcx/datasources/query.go`. Miss it and your kind is rejected
there as unsupported, with no failing test to tell you.

## What the root conformance suites do to your command

| Suite | What it proves — and the common failure |
|-------|------------------------------------------|
| `TestAgentConformance_EveryFiniteLeafEmitsOneJSONValue` | Runs every finite/artifact/stream leaf with NO arguments in a fully isolated environment (empty HOME, closed stdin, 20s timeout, agent mode on). Failures: cobra usage text on stdout instead of one JSON error document; a prompt or editor that blocks (interactive paths must not block in agent mode); in-band `exitCode` disagreeing with the process exit code. |
| `TestConsistency_*` family | The wiring table above. The error text names the file to edit. |
| `TestSkillsGcxInvocationsMatchCommandTree` | Every gcx invocation in bundled-skill markdown — shell fences and inline backtick spans, `gcx` and `bin/gcx` forms — must resolve against the real tree. |

These enforce wiring and protocol shape only. Request mapping, filtering
semantics, pagination and error taxonomy are proven by YOUR package tests
(self-review.md T8).

## Preflight

Format what you actually edited — including changes you have not committed yet,
which a base-diff would miss — then gate:

```bash
gofmt -w <the .go files you edited>
mise run gate
GCX_AGENT_MODE=false mise run all
```

`mise run gate` is the fast inner loop (lint + tests + build).
`GCX_AGENT_MODE=false mise run all` is the pre-push gate and already subsumes
validate-skills, lint, tests, build and docs — you do not need to run those
separately as well.

For a skill-only change:

```bash
mise run validate-skills
go test ./cmd/gcx/root/ -run TestSkillsGcxInvocations
```

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
| `TestConsistency_AllLeafCommandsHaveTokenCost` / `NonSmallCommandsHaveLLMHint` | Missing agent annotations | Registry entry in `internal/agent/command_annotations.go`, or inline `cmd.Annotations` |
| `TestAgentConformance_EveryFiniteLeafEmitsOneJSONValue` fails or times out | Usage text on stdout instead of one JSON document, or a prompt/editor survives agent mode | Return the in-band error document; never block in agent mode |
| `TestSkillsGcxInvocationsMatchCommandTree` | A bundled-skill edit references an unknown command or flag | Fix the invocation, or use a non-shell fence / a `<placeholder>` for hypotheticals |
| `mise run validate-skills` | Skill front matter missing `name` or `description` | Fix the front matter |
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
