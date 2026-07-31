---
name: integrate-with-gcx
description: >
  Guides an engineer and their coding agent through contributing a new
  capability to the gcx codebase itself: deciding whether a new command is
  needed at all, choosing the backend surface and gcx wiring (provider,
  datasource kind, signal command, resource adapter, or bundled skill),
  passing a backend-readiness gate, writing an explicit integration contract
  (naming, typed inputs, output class, completeness and limits, errors, token
  cost), implementing with shared-infra reuse, and self-reviewing against
  recurring review findings before human review. Use when working inside the
  grafana/gcx repository to add or extend a capability. Trigger on phrases
  like "integrate with gcx", "add my product to gcx", "new gcx command",
  "expose this API through gcx", or "get my gcx PR ready for review". NOT for
  operating a Grafana instance with gcx — use the gcx skill or a product skill
  (slo-manage, synth-manage-checks, create-dashboard). NOT for porting a
  provider from grafana-cloud-cli — use the repo's migrate-provider workflow.
---

# Integrate a Capability with gcx

Contribute a new capability to the gcx codebase — as a domain engineer or the
coding agent working with one — through six phases: surface necessity →
placement and readiness → integration contract → implementation → self-review →
preflight and PR summary. Work happens inside a grafana/gcx checkout.

The premise everything below follows from: **the commands you add are tools
that other agents will route on.** Their `Use`/`Short`/`Long`/`Example`, flag
help, token cost, and hints are surfaced verbatim through `gcx commands` and
`gcx help-tree` — that metadata is an operational contract, not decoration.

## Core Principles

1. **Hard invariants live in CONSTITUTION.md and CI — this skill is the map,
   not the law.** The compliance order is CONSTITUTION > VISION > DESIGN
   (`docs/design/`) > ARCHITECTURE. When this skill and a governing doc
   disagree, the doc wins; report the discrepancy.
2. **Released names are frozen within a major version.** Choose every command
   path as if it were permanent — a conforming replacement can be added later,
   but removal waits for the next major. One doc read
   (`docs/design/command-naming.md`) is cheaper than review rounds.
3. **Honesty over convenience.** Every limit, cap, client-side filter, and
   token-cost claim must be visible to the caller. Absent truncation metadata
   means "complete" — make it true.
4. **Reuse before writing.** gcx already has shared config loading, query
   transport, output codecs, truncation metadata, and confirmation helpers.
   A hand-rolled copy misses every future bug fix in the shared version.
5. **Fix pushes are a major defect source.** Re-run the scoped self-review
   after every push, not just before the first review (SR-9).
6. **Humans own architecture.** The placement memo and the contract worksheet
   each get an explicit human sign-off before code. Escalate judgment calls as
   open questions instead of settling them silently.

## Prerequisites

A working grafana/gcx checkout:

```bash
mise run build
bin/gcx commands >/dev/null && echo ok
```

Read `AGENTS.md` at the repo root first — it is the entry-point map to every
document this skill cites.

## Phase A: Surface necessity, placement, readiness

> Details and templates: [references/placement-and-readiness.md](references/placement-and-readiness.md)

**A0 — Is a new surface needed at all?** Inventory the existing tree before
proposing anything:

```bash
gcx help-tree
gcx commands --flat -o json
gcx resources list-types
```

Decide: reuse / extend an existing leaf / consolidate with a sibling / new
independent leaf / no new surface. For a new leaf, name the nearest existing
sibling and write the one-sentence rule an agent would use to pick between
them. If a human can't say definitively which command applies, an agent can't.

**A1 — Backend surface.** What serves the data: Grafana K8s API (`/apis`),
plugin/product REST API, GCOM control plane, a datasource query API, or no
backend (client-only workflow)? Verify with a probe, don't assume.

**A2 — gcx wiring.** gcx has two tiers (K8s resources, Cloud providers).
Wiring options: no new code (dynamic resource discovery) · cloud-tier command ·
provider commands (plain commands are first-class; adapter-backed resources
and cross-signal `signals.Descriptor` commands are patterns within a provider,
used only where they fit) · datasource provider · skill-only. `gcx api` is a
diagnostic fallback, never the integration target. An adapter is never created
merely to unlock a CRUD verb.

**A3 — Backend readiness.** Product teams own their API shape, auth/RBAC,
limits, and domain data reduction. Check owner, stability, auth boundary,
rate/tenant limits, pagination/failure semantics, and whether gcx would have to
bulk-page data client-side to compute what the backend should compute.
Outcome: **ready** / **backend prerequisite (named owner)** / **bounded
bootstrap** (explicitly experimental, no invented public contract) /
**not gcx** (write a boundary memo, not code).

**Deliverable:** the placement memo (template in the reference) — human
sign-off before Phase B.

## Phase B: Integration contract worksheet

> Template and field guidance: [references/contract-and-tests.md](references/contract-and-tests.md)

Fill the worksheet completely before implementing. It forces the decisions
reviews otherwise extract one round at a time:

- Purpose, stability (stable vs experimental), direct/indirect use signals,
  when-NOT-to-use + nearest-sibling distinction.
- Command path derived from `docs/design/command-naming.md` (placement is per
  operation; a discovery facet with no addressable item takes an
  `<operation>-<subject>` compound). Check the real tree for precedent.
- `Use`/`Short`/`Long`/`Example` drafted as routing metadata; parameter names
  consistent with sibling vocabulary; every input typed with constraints, a
  defaulted-for-a-reason value, an example, and explicit empty-value behavior;
  an `Args:` validator on every leaf.
- Output protocol class (one of the eight in `docs/design/agent-mode.md`
  §6.4), a success-schema sketch, and one representative agent-mode result.
- Backend request mapping: endpoint, which flag feeds which param, pagination.
- Completeness contract: complete / limited / capped — and which
  `list_meta` constructor implements it. Silent truncation is forbidden.
- Error contract: summary vocabulary + exit code per expected failure; invalid
  input reports the rejected value, expected format/allowed values, and a
  corrected call; retryability noted where the backend rate-limits.
- Expected data size → `token_cost` (+ a narrowing-oriented `llm_hint` for
  medium/large).
- Auth/ownership boundary, exact shared packages to reuse, explicit non-goals.
- The 5-row agent-routing test matrix (positive / near-miss / ambiguous /
  malformed / large-result).

**Deliverable:** the filled worksheet — human sign-off before Phase C.

## Phase C: Implement with reuse

Search for the shared implementation before writing one:

```bash
grep -rn "LoadContextAndConfig" internal/datasources/query/
grep -rn "func NewClient" internal/query/grafanaquery/
grep -rn "BindListLimit\|AttachListMeta" internal/output/
grep -rn "ConfirmDestructive" internal/providers/
```

Rules that are enforced in review but written almost nowhere else:

- **Every leaf declares an `Args:` validator** (`cobra.NoArgs` for flags-only
  commands) — otherwise stray positionals are silently ignored and the command
  answers a different question than asked.
- **Explicitly-empty string flags are usage errors**: detect with
  `cmd.Flags().Changed(...)`, never `value != ""`. An empty `--contains` from
  an unset shell variable must not return the unfiltered set.
- **Sibling commands agree on validation location**: if one parses input
  client-side with a quoted error, its sibling must not proxy the same input
  to the server for an opaque 400.
- **Codecs are registered in `setup(flags)` AND reachable from `RunE`** — a
  registered codec bypassed by a direct format call is dead code.

Everything else follows the governing docs: options pattern and provider steps
(`docs/reference/provider-guide.md`), datasource reuse rules (AGENTS.md Key
Conventions), output and truncation (`docs/design/output.md` §11–§15), safety
(`docs/design/safety.md`), errors (`docs/design/errors.md`).

Close the phase by reading your command back the way agents will see it:

```bash
mise run build
bin/gcx help-tree
bin/gcx commands --flat -o json
```

— and re-check the routing contract against what actually renders.

## Phase D: Self-review (SR-1…SR-11)

> Full checklist with checks and fix patterns: [references/self-review-findings.md](references/self-review-findings.md)

Run all eleven before requesting review: SR-1 token-cost/limit honesty ·
SR-2 list truncation and `list_meta` · SR-3 input validation · SR-4 shared-code
reuse · SR-5 docs regeneration and help rendering · SR-6 naming vs the frozen
surface · SR-7 tests that cannot fail · SR-8 filtering-semantics honesty ·
SR-9 **scoped re-review after every fix push** · SR-10 scope and disclosure ·
SR-11 description rot.

SR-9 is the highest-leverage habit: in recent integrations, fix commits have
repeatedly introduced new findings — including outright blockers — on top of
what they fixed. After every push, resolve the actual PR base (stacked
branches!) and re-run SR-1…SR-8 over both the full and the incremental diff.

## Phase E: Preflight

> Wiring table and CI-suite meanings: [references/distribution-and-gates.md](references/distribution-and-gates.md)

The per-leaf wiring CI will not let you skip: an `output_classes.json` entry,
a token-cost annotation (+ hint if medium/large), availability/skill mappings
where applicable, regenerated reference docs, and the package-map row. Then:

```bash
mise run lint
go test ./cmd/gcx/root/... ./internal/agent/...
go test ./...
GCX_AGENT_MODE=false mise run reference
GCX_AGENT_MODE=false mise run all
```

`GCX_AGENT_MODE=false` is load-bearing — agent-mode detection flips output
defaults and corrupts generated docs. A gate you cannot run locally is
reported as SKIPPED with the reason, never as green. The authoritative
checklists are in AGENTS.md (Mandatory Pre-Commit / Pull Request).

## Phase F: PR-ready summary

## Output Format

End with this summary (it doubles as the PR description skeleton):

```text
INTEGRATION SUMMARY — <capability>

Placement: <surface + wiring> — <two-line rationale, alternative considered>
Readiness: <outcome; boundary items and their owners>
Contract: <worksheet — final version, linked or inlined>

Self-review attestation:
  SR-1..SR-11: pass | n/a — one line of evidence each
  Routing matrix: verified | UNVERIFIED (no harness)

Open questions for reviewers: <judgment calls deliberately not settled here>
Non-goals / deferred: <explicitly out of scope; product-owned items with owners>
Docs touched: <generated + hand-written>
Gates: <each gate: green | SKIPPED (reason)>
```

Report honestly: a skipped gate is SKIPPED, an unverified matrix is
UNVERIFIED, and open questions belong to the reviewers, not to silence.

## Error Handling

| Failure | Meaning | Fix |
|---------|---------|-----|
| `TestConsistency_AllLeafCommandsHaveOutputClass` | New leaf missing from the class fixture (or a stale entry after a rename) | Add/update the entry in `cmd/gcx/root/testdata/output_classes.json` |
| `TestConsistency_AllLeafCommandsHaveTokenCost` / `NonSmallCommandsHaveLLMHint` | Missing agent annotations | Registry entry in `internal/agent/command_annotations.go` or inline `cmd.Annotations` |
| `TestAgentConformance_EveryFiniteLeafEmitsOneJSONValue` fails or times out | Usage text on stdout instead of one JSON document, or a prompt/editor survives agent mode | Return the in-band error document; decline interactivity in agent mode |
| `TestSkillsGcxInvocationsMatchCommandTree` | A bundled-skill markdown edit references an unknown command/flag | Fix the invocation, or use a text fence / `<placeholder>` for hypotheticals |
| `mise run reference-drift` (CI) | Generated docs not regenerated | `GCX_AGENT_MODE=false mise run reference`, commit the diff |
| Local `mise run lint` reports findings in other worktrees | Stale golangci-lint cache | `mise exec -- golangci-lint cache clean`, re-run |

## Related Skills

- **gcx** — operating Grafana through gcx (resources, queries, workflows), not
  changing gcx itself.
- **setup-gcx** — installing and configuring gcx for an end user.
- Product skills (`slo-manage`, `synth-manage-checks`, `create-dashboard`, …)
  — using specific Grafana products via gcx.

## References

- [references/placement-and-readiness.md](references/placement-and-readiness.md) — surface necessity, the two axes, readiness gate, memo template
- [references/contract-and-tests.md](references/contract-and-tests.md) — the worksheet, field guidance, routing matrix, test-plan bar
- [references/self-review-findings.md](references/self-review-findings.md) — SR-1…SR-11 with concrete checks
- [references/distribution-and-gates.md](references/distribution-and-gates.md) — per-leaf wiring, CI suites, preflight
- In-repo: `AGENTS.md`, `CONSTITUTION.md`, `docs/design/command-naming.md`, `docs/design/output.md`, `docs/design/agent-mode.md`, `docs/reference/provider-guide.md`
