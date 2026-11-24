---
name: kg-agent-test
description: >-
  Tests what the Grafana Knowledge Graph does for an agent that works through
  gcx: runs the user's own questions with and without the graph, records time,
  cost and correctness, and produces a scorecard they can optionally share.
  Use when someone wants to know whether the Knowledge Graph helps their agent
  on their own stack.
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
- **Don't change the prompts.** The with-graph prompt deliberately says nothing
  about the graph, so the result shows what an agent does with gcx as it ships.
- **Don't send anything without an explicit yes.** Sharing is optional. Show the
  user the exact payload first (`share.py --dry-run`), and only then run it with
  `--yes` if they say so.
- **Tell the user what a run will cost before starting it** (the dry run prints
  the run count; roughly $0.15 to $0.40 per run in our tests, more for
  incident questions).

## Steps

1. **Check prerequisites.** `gcx --version`, `gcx config list-contexts`,
   `claude --version`, `python3 --version` (3.9 or later).
2. **Copy the template.** `cp $SKILL/questions.example.json my-questions.json`.
   The five core questions run by default; optional extras run with `--all`.
   Help the user fill in their services, environments, a finished hour for
   `window`, and any `stack_notes` they'd tell a colleague (for example which
   Prometheus datasource to use).
3. **Write the answer keys with the user.** For each question: what a correct
   answer must include, must not include, and any alternative names. For the
   incident question (`I1`), use a past incident with a written root cause and
   keep `block_commands: ["irm", "oncall"]`, or delete it if they don't have
   one. This is the hardest step: check each key against raw telemetry under
   every name a component goes by.
4. **Check graph health.** `python3 $SKILL/kgkit/health.py my-questions.json --out results/`.
   Green: go ahead. Amber: go ahead and note the warnings (an out-of-date gcx is
   amber; suggest upgrading). Red: stop and fix the graph with the
   `diagnose-entity-graph` skill, or pick other questions.
5. **Dry run, then run.** `python3 $SKILL/kgkit/run.py my-questions.json --out results/ --dry-run`
   to show the prompts and run count; get the user's OK on cost; then the same
   command without `--dry-run`. Runs take a while; let them finish.
6. **Score.** `python3 $SKILL/kgkit/score.py my-questions.json --out results/`.
   Show the user `results/report.md`, then summarise in plain words: questions
   won, time and cost with and without the graph, correctness, and any runs
   discarded because the without-graph configuration reached the graph. If a
   result looks wrong, check the answer key before blaming either configuration.
7. **Offer to share.** `python3 $SKILL/kgkit/share.py results/scorecard.json --questions my-questions.json --dry-run`
   shows exactly what would be sent (counts and scores, no service names or
   answers). Send it only if the user says yes.

## Other agents

`run.py` drives Claude Code. With another agent, follow
[references/guide.md](references/guide.md) → "Other agents": put `$SKILL/kgkit/bin`
first on `PATH`, set `KGKIT_REAL_GCX`, `KGKIT_MODE` and `KGKIT_LOG`, run the
prompts that `run.py --dry-run` prints in fresh sessions, and score by hand.
