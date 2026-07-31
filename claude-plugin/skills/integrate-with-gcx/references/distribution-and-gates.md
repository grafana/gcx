# Wiring, Gates, and Preflight

## Per-leaf wiring that cannot be skipped

Adding a command to the tree is automatic (provider `Commands()`, datasource
registration, or signal descriptor). The following are NOT automatic, and CI
fails without them:

| Wiring | Where | Enforced by |
|--------|-------|-------------|
| Output protocol class | `cmd/gcx/root/testdata/output_classes.json` — one entry per new leaf | `TestConsistency_AllLeafCommandsHaveOutputClass` (also fails on stale entries after renames) |
| Token cost | `internal/agent/command_annotations.go` registry entry, or inline `cmd.Annotations` (signal specs set it via `CommandSpec.TokenCost`) | `TestConsistency_AllLeafCommandsHaveTokenCost` |
| LLM hint (medium/large costs) | same as token cost | `TestConsistency_NonSmallCommandsHaveLLMHint` |
| Cloud-only availability | `internal/agent/availability.go` (path prefix, covers subtrees) | `TestConsistency_CloudOnlyPathsResolveToCommands` |
| Resource-type agent metadata | adapter `Registration.Operations` (adapter-backed) or `internal/agent/known_resources.go` (native K8s types) | `internal/agent/known_resources_test.go` |
| Generated reference docs | `GCX_AGENT_MODE=false mise run reference` | `mise run reference-drift` in CI |
| Package map | `docs/architecture/project-structure.md` | AGENTS.md PR checklist step 4 (human-enforced) |

## What the root conformance suites will do to your command

Every leaf is swept automatically — these run in CI whether or not you think
about them:

| Suite | What it proves — and the common failure |
|-------|------------------------------------------|
| `TestAgentConformance_EveryFiniteLeafEmitsOneJSONValue` | Runs every finite/artifact/stream leaf with NO arguments in a fully isolated environment (empty HOME, closed stdin, 20s timeout, agent mode on). Failure modes: cobra usage text on stdout instead of one JSON error document; a prompt or editor that blocks (interactive paths must decline in agent mode); in-band `exitCode` disagreeing with the process exit code. |
| `TestConsistency_*` family | The wiring table above. Error text names the file to edit. |
| `TestSkillsGcxInvocationsMatchCommandTree` | If you also touch bundled skills: every gcx invocation in skill markdown (shell fences AND inline backticks) must resolve against the real tree. |

These enforce wiring and protocol shape only. Request mapping, filtering
semantics, pagination, and error taxonomy are proven by YOUR package tests
(see contract-and-tests.md §5).

## Preflight sequence

Run before requesting review, and again before every push:

```bash
gofmt -w $(git diff --name-only origin/main...HEAD -- '*.go')
mise run lint
go test ./cmd/gcx/root/... ./internal/agent/...
go test ./...
GCX_AGENT_MODE=false mise run reference
GCX_AGENT_MODE=false mise run all
```

Notes:
- `GCX_AGENT_MODE=false` matters: agent-mode auto-detection flips output
  defaults and silently produces wrong generated docs from agent environments.
- If a tool is unavailable locally (e.g. mkdocs for the docs build), report
  that gate as SKIPPED with the reason — never as green. CI covers it.
- The full checklists live in AGENTS.md (Mandatory Pre-Commit / Pull Request
  Checklists) — this sequence is the command form, not a replacement.

## Inspect the built contract the way agents will

After implementing, read your command back through the agent surface:

```bash
mise run build
bin/gcx commands --flat -o json
bin/gcx help-tree
```

Check your leaf's entry: description, args, flags, token cost, hint. This is
the exact metadata agents route on — if it reads ambiguous next to its
nearest sibling, fix it now (SR-11).
