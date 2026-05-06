# Playbook — Spike an ADR for buildability

**Use when**: an ADR has been drafted with multiple decisions but it isn't yet clear whether each decision is actually achievable against the real backend / existing code. The spike is the cheapest way to find that out before committing to the implementation work.

**Output**: a per-decision GREEN / YELLOW / RED verdict backed by real captured evidence (endpoint shapes, timing, integration variations) — plus a runnable POC subcommand per decision that future implementation can crib from. **Not production code**.

**Reference example**: `docs/research/oncall-spike/` — D1–D8 of ADR 001 (oncall feature expansion).

## When this is the right tool

| ✅ Good fit | ❌ Bad fit |
|---|---|
| ADR with 5+ decisions, mixed risk levels | Single small bug fix |
| Decisions depend on backend behaviour you haven't verified | Pure refactoring with no external dependency |
| ADR text references API fields that may or may not exist | Architecture doc with no implementation claims |
| Fast feedback would change the ADR | ADR is already implemented and we're just QA'ing |

## The 7-step shape

### 1. Read the ADR end-to-end and risk-rank decisions

Skim once for context, then re-read identifying:
- **High-risk** decisions: depend on backend behaviour, claim specific API fields exist, propose new contracts. These need spikes.
- **Low-risk** decisions: pure client-side composition, mechanical wiring, established patterns. Can fold into one combined demo.

For ADR 001, D2 (rich payload), D4 (notifications send), D5 (shifts list filter composition) were high-risk; D1, D3, D6, D7, D8 were low-risk client-side and folded into one combined demo.

### 2. Orient against existing code AND upstream system source

Both directions matter:

- **Existing client code**: how does the current implementation work? What types exist, what endpoints are called, what auth path?
- **Upstream backend source**: what does the API *actually* return, what filters does it support, what enum values? The ADR may make claims that don't match reality.

For ADR 001 the IRM repo (`/Users/igor/Code/grafana/irm/`) was the source of truth for what fields the OnCall internal API actually returns. Reading `apps/api/serializers/alert.py` revealed that the slim `AlertSerializer` is used on list endpoints — invalidating the ADR's premise that `list-alerts` returns the rich payload.

### 3. Run quick smoke tests against a real stack

Before designing the spike, hit the existing CLI against a real environment to capture actual data shapes. This grounds the design in reality and surfaces concrete test IDs (alertgroup PKs, schedule IDs) you'll thread through agent briefs.

```bash
gcx --context=ops irm oncall alert-groups list --max-age 24h --output json | head -30
gcx --context=ops irm oncall schedules list --output json
gcx --context=ops irm oncall alert-groups list-alerts <some-group-id>
```

Capture the IDs you'll reuse: the user's example AlertGroup, schedules with someone on call now, etc.

### 4. Call advisor BEFORE committing to a spike plan

After orientation, before spawning agents, call `advisor()`. The advisor sees your full transcript and will:
- Refine your risk-ranking (fewer agents than you thought, usually)
- Flag headline findings to surface in the brief (e.g. "the AlertSerializer asymmetry is the #1 thing")
- Catch overlap risk (parallel agents touching shared files)
- Suggest a conflict-proof file layout

This is high-leverage. The spike's quality starts here.

### 5. Build a conflict-proof scaffold

Each agent gets its own file. No shared writes.

```go
// internal/providers/irm/spike.go
package irm
import "github.com/spf13/cobra"

var spikeBuilders []func(OnCallConfigLoader) *cobra.Command

func registerSpikeBuilder(b func(OnCallConfigLoader) *cobra.Command) {
    spikeBuilders = append(spikeBuilders, b)
}

func newSpikeCmd(loader OnCallConfigLoader) *cobra.Command {
    cmd := &cobra.Command{Use: "spike", Hidden: true}
    for _, b := range spikeBuilders {
        cmd.AddCommand(b(loader))
    }
    return cmd
}
```

Each spike file (`spike_d2_*.go`, `spike_d4_*.go`, …) calls `registerSpikeBuilder(buildXCmd)` in `init()`. The parent command auto-discovers them. **Zero merge conflicts** because no agent edits a shared file.

Wire the parent into the existing command tree once — that edit is yours, before any agent starts.

### 6. Spawn parallel agents with self-contained briefs

One agent per high-risk decision, plus one for the combined low-risk demo. Use `executor` (Sonnet) — Opus is overkill for spike-level work.

Each brief MUST include:
- The exact ADR section being validated, quoted or summarized.
- Real test IDs from your smoke tests, with notes on what each represents (different integration types, different states).
- The exact API endpoint(s) under investigation, with sample paths and expected query params.
- The output contract — strictly: a runnable Cobra subcommand registered via the scaffold + a markdown verdict report at a unique path.
- The verdict template — GREEN / YELLOW / RED with explicit definitions ("GREEN = ADR claim holds, ship as written. YELLOW = ships with caveat — name it. RED = blocked on backend change to X.").
- Quality bar: no tests, `panic(err)` is fine, no architecture polish, hard-code where useful. Make this explicit; otherwise agents over-engineer.
- Constraints: do NOT modify the scaffold or any other agent's file. Do NOT modify the ADR. Do NOT touch unrelated code.

Run agents in parallel — single message, multiple `Agent` tool calls, all `run_in_background: true`. Don't poll; you'll be notified.

### 7. Aggregate verdicts into a single REPORT.md

When all agents complete, write one `REPORT.md` with:
- TL;DR table: per-decision verdict + effort + one-sentence note linking to the per-finding doc.
- Headline findings: the 3–5 things that genuinely change how the ADR ships.
- Cross-cutting observations not in any single agent's report (e.g. naming overlaps, side findings, production bugs).
- Open questions for the ADR author.
- Required ADR text edits.

While agents work in parallel you can do orthogonal investigation — e.g. checking whether the cross-provider pivot targets they're producing actually land somewhere useful (`gcx alert rules get`, `gcx alert instances list --rule`).

## Decision tree for after the spike

| Verdict pattern | Next move |
|---|---|
| All GREEN | Implementation is straightforward. Skip directly to the per-finding iterate-and-ship workflow ([`iterate-finding-to-shipped.md`](iterate-finding-to-shipped.md)). |
| Mostly GREEN, 1–2 YELLOW with named caveats | Pick the YELLOW with the biggest reach, iterate on it first. Caveats become design questions. |
| Mostly YELLOW or any RED | Stop and have the ADR author / sponsor read the report. Some decisions may need rework or removal before implementation makes sense. |

## Anti-patterns

- **Spawning one agent per ADR decision.** 4 agents covering 8 decisions is usually right; one per decision means duplicated context-loading and brittle reports.
- **Letting the agent decide quality bar.** Without "no tests, no architecture polish" in the brief, agents over-build and the spike takes 3× longer.
- **Skipping the orientation phase.** Going straight from ADR to agent dispatch leads to agents discovering basic facts on the live stack instead of you giving them ground truth in the brief.
- **Polling agent progress.** You'll get a notification when each completes; do orthogonal work in the meantime, don't burn context tailing JSONL transcripts.
- **Treating the spike code as production-ready.** It's POC. The implementation phase rewrites it cleanly. The spike's value is the verdict + the proven approach, not the code.

## Artifacts produced

- `docs/research/<spike-name>/REPORT.md` — aggregate verdict with TL;DR table, headline findings, ADR edits required.
- `docs/research/<spike-name>/d{N}-*.md` — per-decision verdict reports (one per agent).
- `<package>/spike.go` + `<package>/spike_d{N}_*.go` — runnable POC subcommands, hidden in the CLI tree.
- Optional: a beads epic + child tasks tracking spike work.

The spike code stays in the repo as a reference implementation throughout the iterate-and-ship phase. It gets removed only after the ADR is fully shipped (or earlier if the spike turns out RED and the decision is dropped).
