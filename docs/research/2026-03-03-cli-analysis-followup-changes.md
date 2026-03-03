# CLI Analysis Follow-Up: Required Changes for grafanactl

**Date:** 2026-03-03
**Source:** Cross-reference of `cli-analysis.md` (gh vs pup comparative analysis) against `agent-docs/` architecture documentation
**Purpose:** Identify specific follow-up changes needed in grafanactl to improve agentic experience

---

## Executive Summary

The cli-analysis.md identified 10 patterns to adopt and 13 prioritized recommendations (R1.1-R3.5). Cross-referencing these against our agent-docs reveals:

- **3 recommendations are partially addressed** by existing code (output formatting, config env vars, error handling)
- **10 recommendations require new work** (exit codes, agent mode, auto-approve, help pages, JSON field discovery, API escape hatch, pipe detection, idempotency docs, in-band errors)
- **2 gaps in agent-docs** exposed by the analysis (no documentation of exit code behavior, no agent integration guidance)

---

## Cross-Reference Matrix

### Priority 1: Critical for Agent Reliability

| Recommendation | Current State (from agent-docs/code) | Gap | Action Required |
|---|---|---|---|
| **R1.1 — Document exit codes** | `cmd/grafanactl/fail/detailed.go` has `ExitCode *int` field on `DetailedError`, but no documented taxonomy. `main.go` uses `os.Exit(1)` as default. No differentiated exit codes for auth vs. command failure vs. version mismatch. | **Large gap** — exit codes exist as a mechanism but are undocumented and unexploited | Implement exit code taxonomy (0/1/2/3/4), document in `help exit-codes`, update `main.go` error handler |
| **R1.2 — Add `GRAFANACTL_AUTO_APPROVE`** | No confirmation prompts exist currently for destructive operations (`delete`, `push`). `delete` command has no `--yes` flag. Per agent-docs: "Deleter does NOT check IsManaged()" — no confirmation before deletion. | **Medium gap** — no prompts exist today, but they should be added before GA along with bypass mechanism | Add confirmation prompts for `delete` and `push --overwrite`, then add `--yes`/`-y` flag + `GRAFANACTL_AUTO_APPROVE` env var |
| **R1.3 — Add agent mode detection** | No agent mode concept. `--no-color` flag exists (root command), `--output/-o` flag exists per command. No detection of `CLAUDE_CODE`, `CURSOR_AGENT`, etc. environment variables. | **Large gap** — no agent awareness at all | Implement agent mode package: detect env vars, auto-set JSON output, suppress color/spinners, auto-approve |

### Priority 2: High Value Documentation

| Recommendation | Current State | Gap | Action Required |
|---|---|---|---|
| **R2.1 — `help formatting` page** | Output formatting exists: `-o json/yaml/text/wide` via `io.Options` (documented in `cli-layer.md`). `get.go` uses `k8s.io/cli-runtime/pkg/printers.NewTablePrinter`. No dedicated help topic. | **Documentation gap only** — code exists, docs don't | Create `grafanactl help formatting` with examples for each output mode, jq piping patterns |
| **R2.2 — `help environment` page** | Env vars documented in `config-system.md` agent doc: `GRAFANA_SERVER`, `GRAFANA_TOKEN`, `GRAFANA_USER`, `GRAFANA_PASSWORD`, `GRAFANA_ORG_ID`, `GRAFANA_STACK_ID`, `GRAFANACTL_CONFIG`. Also `--no-color` in root. Not consolidated into user-facing help. | **Documentation gap** — vars exist but no user-facing reference | Create `grafanactl help environment` consolidating all env vars with descriptions |
| **R2.3 — Automation Guide** | No dedicated automation/CI-CD guide. Agent-docs are for coding agents, not for CI/CD pipeline users. | **Content gap** | Create automation guide: CI/CD patterns, headless auth, scripting with JSON output, exit code handling |

### Priority 3: Enhanced Capability

| Recommendation | Current State | Gap | Action Required |
|---|---|---|---|
| **R3.1 — JSON field discovery** | `--json` not supported as a special flag. `-o json` outputs full resource. No mechanism to list available fields or select specific fields. Resources are `unstructured.Unstructured` (dynamic), so field schema comes from the API, not from Go types. | **Feature gap** — need new code | Add `--json [fields]` flag: no args = list available top-level fields, with args = select specific fields. Challenge: unstructured objects have dynamic schemas |
| **R3.2 — `grafanactl api` escape hatch** | No raw API command. All API access goes through typed commands. Auth context exists and could be reused. | **Feature gap** | Implement `grafanactl api [path]` command using existing auth/config infrastructure |
| **R3.3 — Pipe-aware output switching** | `--no-color` exists as manual flag. No pipe detection. Color is disabled via `color.NoColor = true` when `--no-color` is set (root command `PersistentPreRun`). | **Feature gap** — need pipe detection | Add `os.Stdout.Fd()` + `term.IsTerminal()` check in root `PersistentPreRun`; auto-disable color when piped |
| **R3.4 — Document push idempotency** | Push IS idempotent (upsert): `pusher.go` does Get → if exists: Update, if 404: Create. This is documented in `data-flows.md` but not in user-facing docs. | **Documentation gap only** — behavior exists, not documented for users | Add explicit "Push is idempotent (create-or-update)" to push command help and automation guide |
| **R3.5 — In-band error reporting** | Errors go through `DetailedError` → stderr rendering. JSON output only contains resource data, never errors. In agent mode, errors would need to be wrapped in the JSON response body. | **Feature gap** — needs agent mode first | After agent mode (R1.3): wrap errors in JSON response body with `errors[]` and `hints[]` fields |

---

## Gaps in Agent Documentation

The cross-reference also exposed documentation gaps in `agent-docs/` that should be fixed regardless of feature work:

| Gap | Current State | Fix |
|---|---|---|
| **Exit code behavior** | `cli-layer.md` documents error handling chain (`DetailedError` → `ErrorToDetailedError`) but never mentions what exit codes are returned | Add "Exit Codes" section to `cli-layer.md` documenting current behavior |
| **Environment variable reference** | `config-system.md` documents env vars but only the `GRAFANA_*` set. Missing: `GRAFANACTL_CONFIG`, `NO_COLOR` | Add complete env var table to `config-system.md` |
| **Idempotency behavior** | `data-flows.md` describes upsert logic in detail but doesn't use the word "idempotent" | Add explicit note: "Push is idempotent: creates new resources and updates existing ones" |

---

## Recommended Implementation Order

Based on effort/impact analysis and dependency chain:

### Phase 1: Documentation-Only (Low effort, unblocks CI/CD users)

1. **Document exit codes** in user-facing help (R1.1 — docs part)
2. **Create `help environment` page** (R2.2)
3. **Create `help formatting` page** (R2.1)
4. **Document push idempotency** (R3.4)
5. **Fix agent-docs gaps** (exit codes, env vars, idempotency)

### Phase 2: Pipe Detection + Color (Low effort, quick win)

6. **Pipe-aware output switching** (R3.3) — auto-disable color/truncation when piped

### Phase 3: Agent Mode Foundation (Medium effort, highest agentic impact)

7. **Agent mode detection** (R1.3) — detect `CLAUDE_CODE`, `CURSOR_AGENT`, etc.
8. **Agent mode output** — default JSON, suppress color, suppress spinners
9. **Exit code taxonomy** (R1.1 — code part) — implement differentiated exit codes

### Phase 4: Confirmation + Auto-Approve (Medium effort, safety first)

10. **Add confirmation prompts** for `delete` (and optionally `push` with destructive flags)
11. **Add `--yes`/`-y` and `GRAFANACTL_AUTO_APPROVE`** (R1.2)
12. **Auto-approve in agent mode** — agent mode implies `--yes`

### Phase 5: Enhanced Features (Higher effort)

13. **JSON field discovery** (R3.1) — `--json` with no arg lists fields
14. **`grafanactl api` escape hatch** (R3.2) — raw authenticated API access
15. **In-band error reporting** (R3.5) — errors in JSON response body (requires agent mode)
16. **Automation Guide** (R2.3) — comprehensive CI/CD integration guide

---

## Dependency Graph

```
Phase 1 (docs) ──────────────────────────────────────────►
Phase 2 (pipe detection) ─────────────────────────────────►
Phase 3 (agent mode) ─────┬──────────────────────────────►
                           │
                           ├─► Phase 4 (auto-approve) ───►
                           │
                           └─► Phase 5.3 (in-band errors)►
Phase 5.1 (json discovery) ──────────────────────────────►
Phase 5.2 (api command) ─────────────────────────────────►
Phase 5.4 (automation guide) ── depends on Phase 1-4 ───►
```

Phases 1, 2, 5.1, and 5.2 can proceed in parallel. Phase 4 depends on Phase 3. Phase 5.3 depends on Phase 3. Phase 5.4 is best written after everything else.

---

## What's Already Strong (No Changes Needed)

Cross-referencing revealed several areas where grafanactl is already well-positioned:

- **Structured output** — `-o json/yaml/text/wide` is already comparable to gh's offering
- **Config context model** — Directly mirrors kubectl kubeconfig, well-documented in agent-docs
- **Error handling chain** — `DetailedError` with suggestions is already close to pup's hint system
- **Composable processors** — Architecture supports adding agent-mode processors without restructuring
- **K8s resource model** — Dynamic discovery means new resource types work automatically

---

## Patterns from cli-analysis.md Mapped to Our Architecture

| Pattern | Where It Fits in Our Architecture | Effort |
|---|---|---|
| `--json` field discovery | `cmd/grafanactl/io/format.go` — extend `io.Options` | Medium (dynamic schema from unstructured) |
| Agent mode detection | New package `internal/agent/` or extend root command | Medium |
| Exit code taxonomy | `cmd/grafanactl/main.go` + `cmd/grafanactl/fail/detailed.go` | Low (mechanism exists) |
| `help` topics | Cobra has native `AddHelpTopic()` or manual subcommands | Low |
| Pipe detection | `cmd/grafanactl/root/command.go` PersistentPreRun | Low |
| `grafanactl api` | New `cmd/grafanactl/api/command.go` using existing `NamespacedClient` + `httputils` | Medium |
| Auto-approve | Extend `cmd/grafanactl/resources/onerror.go` pattern | Low-Medium |
| In-band errors | Extend `io.Options` codec to wrap errors in response | Medium-High |
| Default JSON in agent mode | Root PersistentPreRun sets default format | Low (after agent mode exists) |
| Hints in responses | Extend `OperationSummary` or `DetailedError` | Medium |

---

---

## Cross-Reference: Extensibility + Ascode Research Findings

This section cross-references findings from two parallel research streams against this document's recommendations:
- [Non-App-Platform Extensibility](2026-03-02-grafanactl-non-app-platform-extensibility.md) (Provider interface, config extension, product CLIs)
- [Ascode Subcommand](2026-03-03-ascode-subcommand.md) (`dev` command group, foundation-sdk integration)

### Synergies

| This Document | Extensibility Finding | Combined Impact |
|---|---|---|
| **R1.3 Agent mode detection** in `PersistentPreRun` | Provider commands inherit root command's `PersistentPreRun` | Agent mode auto-applies to ALL providers (SLO, synth, k6, dev) with zero per-provider work |
| **R2.2 `help environment`** page | `ConfigKeys()` method on Provider interface declares env vars | Auto-generate the environment help page from `providers.All()` + core env vars -- always up-to-date |
| **R1.1 Exit code taxonomy** (code 3 for auth) | Provider `Validate()` returns error before command runs | Exit code 3 (auth failure) unifies: Grafana token invalid, SM token missing, k6 token expired -- all use the same code |
| **R1.2 Auto-approve** (`--yes` flag) | Provider push commands need confirmation for destructive ops | Shared confirm helper pattern: each provider's push calls `confirm()` which respects `--yes` and agent mode |
| **R3.5 In-band error reporting** | Provider commands produce JSON output | Provider errors already structured via `DetailedError` -- wrapping in JSON response body works identically for k8s and REST providers |

### Conflicts Resolved

| Conflict | Resolution |
|---|---|
| **Output format default**: R1.3 agent mode switches default to JSON, but `dev import` outputs Go code | Agent mode sets default output format for *list/get* commands only. Commands with non-data output (import, init, generate) are exempt -- they ignore `-o` flag |
| **Confirm helper ownership**: Both provider push commands and core `resources delete` need confirmation | Shared `internal/confirm` package (or helper in `cmd/grafanactl/resources/`) used by both core commands and providers. Not per-provider. |

### Additional Agent-Docs Gaps (from Extensibility + Ascode Research)

These gaps were identified by cross-referencing the extensibility and ascode research against `agent-docs/`:

| Gap | Source | Affected Doc | Fix |
|---|---|---|---|
| **Provider architecture undocumented** | Extensibility research: Provider interface, registry, config extension | `architecture.md` | Add "Extension Layer" between Business Logic and Client Layer showing Provider interface |
| **Provider command pattern undocumented** | Extensibility research: provider commands differ from `resources` commands | `cli-layer.md` | Add "Provider Command Groups" section explaining how providers contribute commands |
| **Translation adapter pattern undocumented** | Extensibility research: flat JSON <-> k8s envelope translation | `patterns.md` | Add new pattern entry for the adapter pattern (Prepare/Unprepare, toResource/fromResource) |
| **Provider config extension undocumented** | Extensibility research: `Context.Providers` map, `ConfigKeys()` | `config-system.md` | Add provider config section to data model |
| **Dev tooling as provider undocumented** | Ascode research: `dev` command group implemented as Provider | `cli-layer.md` | Note that non-API providers (dev tooling) also use Provider interface |

### Impact on Implementation Order

The extensibility epic (Provider interface, Wave 0-1) should land **before** Phase 3 (Agent Mode) of this document's recommendations. Rationale:
- Agent mode in `PersistentPreRun` automatically propagates to all provider commands
- `ConfigKeys()` enables auto-generated environment help (R2.2)
- Provider `Validate()` provides the natural hook for exit code 3 (auth failure)

Revised dependency:
```
Wave 0 (Provider infra) ──► Phase 1 (docs) + Phase 2 (pipe detection)
                         ──► Phase 3 (agent mode) ──► Phase 4 (auto-approve)
                         ──► Wave 1 (SLO provider)
```

---

*Cross-reference analysis based on: cli-analysis.md (2026-03-02), extensibility research (2026-03-02), ascode research (2026-03-03), agent-docs/ (2026-03-02), CLAUDE.md project conventions*
