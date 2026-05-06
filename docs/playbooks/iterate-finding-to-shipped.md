# Playbook — Iterate a finding from spike verdict to shipped implementation

**Use when**: a spike has produced a YELLOW or GREEN verdict on an ADR finding and the next step is to ship the implementation. The verdict is the starting point, not the design — the design typically evolves through several rounds of user feedback before it's locked.

**Output**: a shipped implementation that emits the locked shape, an updated ADR section reflecting what actually got built, a tracking doc capturing the iteration arc, and a clean commit.

**Reference example**: D2 of ADR 001 (rich `AlertGroup` / `Alert` shapes) — see `docs/research/oncall-spike/d2-implementation.md` for the iteration log.

## Prerequisites

- ADR finding verdict from the spike phase (see [`spike-adr-buildability.md`](spike-adr-buildability.md)).
- Spike code still in the repo as reference.
- A real test stack with concrete IDs to validate against during iteration.

## The 9-step shape

### 1. Tackle ONE finding at a time

Even if the spike covered 8 findings, work them sequentially. Each finding is its own iteration arc. Don't try to ship D2 + D5 in the same session — the design feedback loops are independent and parallel work creates merge anxiety.

For D2 specifically: the YELLOW verdict said "alertgroup-first path sidesteps N+1; ADR has 2 field-location bugs". That's the *starting point*. The shipped design diverged from this through 8 rounds of iteration.

### 2. Open with the spike's headline + offer the user a sub-decision menu

Don't propose a full design upfront. Surface the headline finding and break the work into 2–4 sub-decisions for the user to weigh in on. Each gets a 2–3 sentence framing with a recommendation and the main tradeoff.

Example from D2:

> **D2 — three sub-decisions to make**
> 1. Where does the rich payload live in the Go types? — three options: alertgroup-first, list-alerts-N+1, or both.
> 2. ADR text bug fixes — three concrete edits to source paths.
> 3. Backend AlertSerializer enrichment — pursue or defer?

The user picks the entry point. You don't decide for them.

### 3. Iterate on shape with REAL data in front of you

Each round of feedback should be grounded in actual captured payloads, not theoretical schemas. When the user proposes a new shape, run it against the live stack and surface what falls out:

- "the output is hostile" — pull a real alertgroup, look at the YAML, identify what's noise vs signal.
- "namespace is missing" — mine multiple alertgroups across integration types to see when it's populated and when not.
- "this should be in spec, not status" — explain the K8s convention, point at ordering control as the cleaner solution.

For D2 each round revealed something the previous round didn't anticipate. You can't shortcut this — the iteration IS the design.

### 4. Use exploratory-question format on every design question

Per project convention: 2–3 sentences with a recommendation and the main tradeoff. Present alternatives as something the user can redirect, not a decided plan. Don't implement until they agree.

```
For X we have three options:
- Option A: <one-sentence behaviour> — <upside, downside>
- Option B: ...
- Option C: ...
My pick: <option> because <reason>. Push back if you want a different lens.
```

This keeps the user in the design seat. They drive the shape; you bring evidence and trade-off reasoning.

### 5. Don't trust the ADR — validate against backend source

The ADR may make claims about API behaviour that are wrong. Always cross-check with the upstream system source. For D2:
- ADR claimed `dashboardUID` is in `labels.dashboard_uid_label_name`. Reality (per IRM source): it's in `annotations.__dashboardUid__`. Three text bugs found this way.
- ADR claimed list-alerts returns the rich payload. Reality: only retrieve does. Different serializer.

When you find an ADR text bug, capture it for the ADR-update step. Don't silently work around it.

### 6. After alignment, dispatch one Opus agent for the lift

Implementation lifts often touch 5–8 files in tightly-coupled ways (types, extraction, client, commands, codecs). Splitting across multiple parallel agents creates merge collisions. **One Opus executor** is the right shape.

Brief MUST include:
- The locked shape, verbatim YAML — leave nothing to interpretation.
- The exact files to touch, with one-line reasons each.
- The reference spike file — agents should crib from it, not redesign.
- Acceptance commands — concrete CLI invocations against a real stack the agent can run, with expected behaviour ("returns N items, includes field X, doesn't include Y").
- Constraints: don't touch the ADR (you'll do that yourself), don't touch the spike (it's reference), don't touch sibling-finding code (parallel session may conflict).
- Quality bar: "as-is, can stay loose" if user said so. Or "production quality with tests" if not.

### 7. Smoke-test extensively after the agent returns

Don't trust the agent's "all acceptance commands pass" report. Re-run them yourself with attention to:
- **Multiple integration shapes / payload variations** — the agent likely tested one happy path. You test the diversity.
- **Edge cases** — empty fields, missing relationships, large counts, error paths.
- **The escape hatches** — `--include-raw`, `--slim`, `--limit`, anything that switches behaviour modes.
- **Backward-compat paths** — does the existing list table view still work? Does the spike still work?

Apply small polish fixes inline (single-file Edit). Anything bigger goes back through another agent dispatch.

### 8. Update the ADR alongside the code

The ADR is the source of truth, not historical fiction. After the implementation lands:
- YAML examples must match what the code emits.
- Field source paths in the extraction tables must match the actual extractor.
- Hint copy in the post-result-hint table must use the new field paths.
- The "Rejected Alternatives" section needs reversals when iterations changed direction (e.g. the D2 `alerts get` retention reversed the original removal plan).
- Consequences (positive + negative) should reflect the shipped behaviour.

Grep for stale references: search for the old field names, old verb names, old shape patterns. Each hit is a candidate edit.

### 9. Write a tracking doc + commit cohesively

The tracking doc serves two purposes:
- Reference for future iteration on the same finding ("How to re-open D2" section).
- Template for tackling the next finding ("here's how this one went").

Format the doc with:
- Final shape (locked YAML).
- "How the design evolved" — numbered sections for each iteration round, capturing what changed and why.
- "What landed" — files touched, ADR sections updated, spike artifacts preserved.
- Smoke tests passing (the actual command list).
- "Open / deferred" — known limitations and trade-offs.
- "How to re-open" — concrete entry points for future modifications.

Commit with a Conventional Commits message. Body should capture the WHY, not just the WHAT — what the user gets, what the design tradeoffs were, what stays open.

## Anti-patterns

- **Implementing while the user is still iterating.** A round of "what about X?" feedback should produce a shape proposal, not a code change. Implement only after the user agrees the shape is locked.
- **Skipping the ADR update.** The ADR drifts from reality and the next person wastes a session re-discovering what changed.
- **Hiding ADR text bugs in the implementation.** Surface them, capture them, fix them in the ADR. Silent workarounds rot.
- **Asking the user to choose without giving evidence.** "Option A or B?" with no data behind each choice puts the cognitive load entirely on them. Mine the data, present options with their tradeoffs.
- **Trusting the agent's report.** Their "acceptance commands pass" might mean one happy-path run. Always re-run the test suite yourself, including the diversity cases.
- **Bundling sibling findings into one commit.** D1 + D2 in the same commit is fine if the user explicitly bundles them; otherwise each finding is its own arc, its own commit, its own session.

## Decision tree

| Situation | Move |
|---|---|
| User confirms a sub-decision | Move to the next one. Don't pre-emptively implement. |
| User pushes back on your recommendation | Acknowledge, re-evaluate. Surface what would change your mind ("if X, then I'd switch to your option"). Don't silently switch. |
| User wants a shape that's hard to implement | Surface the cost. Offer alternatives. If they still want it, do it. |
| User asks "can we also do Y?" mid-iteration | Note it. Finish the current sub-decision. Then evaluate Y as its own iteration round. |
| User says "as-is quality is fine" | Accept it explicitly. Don't write tests, don't run lint, just `go build`. |
| Discrepancy between ADR claim and backend reality | Capture for ADR update. Implement what the backend actually does, not what the ADR claims. |

## Output checklist for end of session

- [ ] Code committed with a clear conventional-commits message.
- [ ] ADR section updated to match shipped reality (YAML, field paths, hint copy, rejected-alternatives, consequences).
- [ ] Per-finding implementation log written (`docs/research/<spike-name>/d{N}-implementation.md`) with iteration arc + open items.
- [ ] Spike artifacts preserved untouched.
- [ ] Smoke-test command list documented in the implementation log so the next person can re-verify.
- [ ] `git status` clean. `bin/<binary>` builds.
- [ ] Any side-finding production bugs captured as separate beads/issues.
