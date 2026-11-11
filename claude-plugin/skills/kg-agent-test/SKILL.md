---
name: kg-agent-test
description: >-
  Tests what the Grafana Knowledge Graph does for an agent that works through
  gcx: runs the user's own questions with and without the graph, records time,
  tokens and correctness per question, and writes a short summary report plus a
  scorecard they can optionally share. Use when someone wants to know whether
  the Knowledge Graph helps their agent on their own stack.
---

# Test the Knowledge Graph with your agent

This skill walks a user through a fair with-and-without comparison on their own
stack. You do the running and scoring; the user supplies the questions and,
above all, the correct answers. Read [references/guide.md](references/guide.md)
for the full detail behind each step.

In the commands below, `$SKILL` is this skill's folder (after
`gcx agent skills install kg-agent-test`, that is
`~/.agents/skills/kg-agent-test`). Run them from the user's own working
directory, so their questions and results stay there.

## Rules that keep the test fair

- **Never write an answer key from the graph or from your own investigation.**
  Answers come from the user's knowledge, their code or config, raw telemetry,
  or a post-incident review. A key built from the graph grades the graph
  against itself.
- **Don't change the prompts.** Both sides get the same question, window and
  notes. By default the with-graph side is also told the graph exists and to
  start with it, so the test measures what the graph adds when it is used.
  `--graph-prompt none` tests whether the agent finds the graph on its own.
- **Prefer hard questions.** Multi-hop blast radius, hidden dependencies and
  incidents separate the two sides. Simple lookups are usually answered
  correctly by both, and only show speed.
- **Go easy on the stack.** Runs go one at a time with a pause between them.
  Don't raise `--parallel` on a production stack.
- **Don't send anything without an explicit yes.** Sharing is optional. Show the
  user the exact payload first (`share.py --dry-run`), and only then run it with
  `--yes` if they say so.
- **Tell the user what the runs will spend before starting.** The dry run
  prints the run count; budget roughly $0.15 to $0.40 of model usage per run,
  more for incident questions.

## Steps

1. **Check prerequisites.** `gcx --version`, `gcx config list-contexts`,
   `claude --version`, `python3 --version` (3.9 or later).
2. **Copy the template.** `cp $SKILL/questions.example.json my-questions.json`.
   The core questions run by default; optional extras run with `--all`.
   Help the user fill in their services, environments, a finished hour for
   `window`, and any `stack_notes` they'd tell a colleague (for example which
   Prometheus datasource to use). If the stack is split into business units,
   platforms or teams, add `graph_groups` so the health check covers each one.
3. **Write the answer keys with the user.** For each question: what a correct
   answer must include, must not include, and any alternative names. For the
   incident question (`I1`), use a past incident with a written root cause and
   keep `block_commands: ["irm", "oncall"]`, or delete it if they don't have
   one. This is the hardest step: check each key against raw telemetry under
   every name a component goes by.
4. **Check graph health.** `python3 $SKILL/kgkit/health.py my-questions.json --out results/`.
   Green: go ahead. Amber: go ahead and note the warnings (an out-of-date gcx is
   amber; suggest upgrading). Red: stop and fix the graph with the
   `diagnose-entity-graph` skill, or pick other questions. If a group has
   services but no relationships, questions from it test the gap: keep them
   only if the user wants that measured, and say so in the summary.
5. **Dry run, then a pilot, then run.** `python3 $SKILL/kgkit/run.py my-questions.json --out results/ --dry-run`
   shows the prompts and run count; get the user's OK on spend. Then run one
   question once on each side (`--only <id> --reps 1`) and score it, to catch
   setup problems cheaply. Then run the full set without `--only`.
6. **Score and summarise.** `python3 $SKILL/kgkit/score.py my-questions.json --out results/`.
   Show the user `results/summary.md`: a headline, a summary table for the
   questions where the agent used the graph, one row per question with each
   run's outcome, time, tokens and graph calls, where each side missed, and
   caveats. Then add two or three sentences in plain words. Lead with the
   questions where the graph was used; say plainly where it wasn't, and that
   differences there are run-to-run variation. If a result looks wrong, check
   the answer key before blaming either side. Offer to turn the summary into a
   page they can share; it names their services, so they decide who sees it.
7. **Offer to share.** `python3 $SKILL/kgkit/share.py results/scorecard.json --questions my-questions.json --dry-run`
   shows exactly what would be sent (counts and scores, no service names or
   answers). Send it only if the user says yes.

## Other agents

`run.py` drives Claude Code. With another agent, follow
[references/guide.md](references/guide.md) → "Other agents": put `$SKILL/kgkit/bin`
first on `PATH`, set `KGKIT_REAL_GCX`, `KGKIT_MODE` and `KGKIT_LOG`, run the
prompts that `run.py --dry-run` prints in fresh sessions, and score by hand.
