# Harness Engineering Gap Analysis

**Date:** 2026-03-27
**Source:** [Harness engineering: leveraging Codex in an agent-first world](https://openai.com/index/harness-engineering/) (OpenAI, Feb 2026)
**Context:** Assessing gcx repo against the principles from OpenAI's "harness engineering" post — 5 months of building a product with 0 lines of manually-written code using Codex agents.

## Article Summary

The core thesis: when agents write all the code, the human engineering job shifts to **designing environments, specifying intent, and building feedback loops**. Six principles emerged:

1. **Map, not manual** — ~100-line AGENTS.md as a table of contents with pointers to deeper docs. Progressive disclosure.
2. **Repository as system of record** — All knowledge (product specs, design rationale, core beliefs, tacit knowledge) must live in-repo. If the agent can't find it, it doesn't exist.
3. **Enforce invariants mechanically** — Custom linters with agent-friendly error messages, structural tests enforcing dependency direction, "taste invariants." Documentation alone doesn't keep the codebase coherent.
4. **Application legibility** — Agents can boot the app per worktree, drive the UI via Chrome DevTools, query logs/metrics via LogQL/PromQL. The feedback loop goes beyond "tests pass."
5. **Entropy and garbage collection** — Recurring background agents scan for convention drift, update quality grades, open targeted refactoring PRs. Technical debt paid continuously in small increments.
6. **Throughput changes merge philosophy** — Minimal blocking gates, short-lived PRs, corrections are cheap. (Team-scale decision, not applicable to gcx yet.)

## Gap Analysis

### Strong: Map, Not Manual (Principle 1)

`CLAUDE.md` is 197 lines, structured as a navigational map with pointers to `docs/architecture/` and `docs/reference/`. Progressive disclosure works — deep architecture docs exist for when agents need them, not loaded upfront.

**No gap.**

### Moderate Gap: Repository as System of Record (Principle 2)

**What we have:** Architecture docs, CONSTITUTION.md, DESIGN.md, provider guides. Good technical coverage.

**What's missing:**

- **No product/domain knowledge directory.** Why does gcx exist? What's the product vision? User personas? An agent implementing a feature has no way to reason about product intent — only technical structure. The OpenAI team maintains `PRODUCT_SENSE.md`, `product-specs/`, and `core-beliefs.md`.
- **No indexed decision records.** DESIGN.md captures some decisions, but there's no `docs/decisions/` directory with structured ADRs. When an agent encounters a surprising pattern, it can't discover the rationale.

### Moderate Gap: Enforce Invariants Mechanically (Principle 3)

**What we have:**

- `golangci-lint` — standard Go linting
- Rego-based linting engine — lints Grafana resources (dashboards, alerts), not the gcx codebase itself
- CONSTITUTION.md — documents invariants, not mechanically enforced
- Conventional Commits enforcement via CI (`pr-title.yaml`)

**What's missing:**

- **No structural tests enforcing architecture.** `docs/architecture/patterns.md` describes 18 patterns, but nothing in CI catches violations. An agent can violate dependency direction (e.g., `cmd/` importing deep `internal/` packages incorrectly) and nothing stops it.
- **No custom Go linter rules with agent-friendly remediation messages.** The article specifically calls out writing error messages that inject remediation instructions into agent context. Our golangci-lint gives generic errors with no gcx-specific guidance.
- **CONSTITUTION.md is passive.** It's a document agents *should* read, not a check that *blocks* bad merges. The constitution-reviewer agent exists as a skill but is opt-in, not a CI gate.

### Significant Gap: Application Legibility (Principle 4)

The article describes: bootable per worktree, Chrome DevTools integration, ephemeral observability stack, agents querying logs and metrics to validate behavior.

gcx is a CLI (no UI to drive), but the principle still applies:

- **No "smoke test against a live Grafana" in the agent loop.** `gcx-testenv` skills exist for bootstrapping a k3d cluster, but there's no lightweight feedback loop where an agent builds gcx → runs it against a test instance → observes the result → iterates.
- **No integration/e2e test harness for agents.** `make tests` runs unit tests only. No `make integration` or `make e2e` that an agent can use to verify real API behavior.

The article's key insight: agents need to *see the effect of their changes*, not just pass unit tests.

### Significant Gap: Entropy and Garbage Collection (Principle 5)

The article describes background agents running on a recurring cadence: scanning for deviations, updating quality grades, opening refactoring PRs.

**What we have:** Nothing automated.

- **No quality scoring** (no `QUALITY_SCORE.md` or equivalent tracking per-domain quality grades)
- **No doc-gardening automation.** `docs/reference/doc-maintenance.md` describes a manual process.
- **No recurring convention-drift detection.** `bd doctor --check=conventions` exists in beads, but nothing periodically scans Go code for pattern violations, dead code, or drift from architecture docs.

## Prioritized Recommendations

| Priority | Gap | Principle | Effort | Notes |
|----------|-----|-----------|--------|-------|
| P1 | Structural tests enforcing architecture (import boundaries, dependency direction) | Enforce invariants | Medium | Custom Go analyzer or `go vet` check in CI |
| P1 | CONSTITUTION.md as a CI gate | Enforce invariants | Low | Wire constitution-reviewer into `claude-code-review.yml` |
| P2 | Product/domain knowledge in-repo | Repo as system of record | Low | A few markdown files: vision, personas, product principles |
| P2 | Integration test harness for agent feedback loop | Application legibility | High | Needs test Grafana instance infrastructure |
| P3 | Quality scoring and automated gardening | Entropy / garbage collection | Medium | Recurring CI job + quality tracking doc |
| P3 | ADR directory with indexed decision records | Repo as system of record | Low | Start capturing decisions as they're made |

## Key Takeaway

The biggest philosophical gap: **we document invariants but don't enforce them mechanically.** The article's core lesson is that documentation alone doesn't keep an agent-generated codebase coherent — you need linters, structural tests, and CI gates that block violations. We're in "trust the agent to read the docs" mode. The path forward is promoting rules from docs into code.
