# Test the Knowledge Graph with an agent that uses gcx

This skill measures what the Knowledge Graph does for an agent working with your Grafana Cloud stack through gcx. You ask questions you know the answers to, each several times with the graph and several times without it, and compare correctness, time and tokens question by question.

It takes one to two hours, mostly unattended, and costs roughly $15 to $45 of model usage for five questions at five runs each. Everything runs on your machine against your stack. Nothing is sent to Grafana unless you choose to share the scorecard at the end.

In the commands below, `$SKILL` is this skill's folder, `~/.agents/skills/kg-agent-test` after `gcx agent skills install kg-agent-test`. Run them from your own working directory.

## What you need

- The latest gcx, signed in to your stack (`gcx --version`, `gcx config list-contexts`). A read-only token is enough. The health check in Step 2 compares your version with the latest release and warns if it's older. Pass `--gcx <path>` to the scripts to test a specific binary.
- Claude Code (`claude --version`), for the automated runs. Other agents can follow the manual steps at the end.
- Python 3.9 or later. The scripts use only the standard library.

## Step 1: Pick your questions and write the answers down first

Copy the template (`cp $SKILL/questions.example.json my-questions.json`) and fill it in. Five core questions run by default. They're the ones where the graph has the most to show, plus one control:

| Type | Starter question | Why it's there |
|---|---|---|
| Hidden dependencies | What does `<service>` connect to, including connections that don't appear in its traces? | Some connections show up only in network flows; telemetry alone often misses them |
| Deep blast radius | If the database behind `<service>` went down, what would be affected, up to the entry points? | Several hops, where rebuilding the map from telemetry gets expensive |
| Finding the right system | Which namespace is `<service>` in, in our `<region described in words>` cluster? | Same name, many places, and no label to filter on |
| Incident root cause | An alert fired at `<time>`: what caused it? | The question that matters most. Needs a past incident with a written root cause; delete it if you don't have one |
| Control | How many `<service>` replicas were available? | A plain telemetry question. The graph shouldn't help here, and if it makes things worse you want to know |

At five runs per question per side that's 50 runs: roughly $8 to $15 with a Sonnet-class model, or $15 to $40 with an Opus-class one. The incident question is the expensive one.

The template also has optional extras (callers, dependencies, a cache blast radius, a labelled namespace lookup, nodes, failing dependencies), marked `"optional": true`. Run them with `--all` if you want more coverage. Both sides usually answer these correctly, so they mostly show speed and can't separate the two on accuracy. In our tests, 8 of 12 questions were answered correctly on every run by both sides; a set made mostly of those says little.

**If your stack is split into business units, platforms or teams,** check the graph per group before picking questions (see Step 2). One group can be well connected while another has no relationships at all. Draw questions from the connected groups to test the graph, and from an unconnected one only if you want to measure the gap, labelled as such.

If you'd rather write your own, three places to find good questions:

- A service you own and could draw from memory.
- The dependency question you had to answer in your last incident.
- What a new joiner would ask on their first day.

For each question, write what a correct answer **must include** and what it **must not include** (for example, something the service calls when the question asks what calls it). Anything else the agent lists is ignored. Give alternative names where a component goes by more than one.

**The rule that matters most:** take the answers from your own knowledge, your code or config, or raw traces and metrics. Never from the agent, and never from the graph. Otherwise the test grades the graph against itself.

**Pin the time window.** Pick an hour that has already ended, such as 09:00 to 10:00 UTC this morning, and put it in the file. Every run looks at the same hour, so runs can't disagree because the system changed in between, and your answers are for that hour.

Use `stack_notes` for anything you'd tell a colleague, such as which Prometheus datasource to use. Both configurations see the same notes.

### Optional: an incident question

The question we most want you to try is the hardest one: an alert from a past incident, where you already know the root cause. The template's `I1` entry shows the shape:

- Pick an incident with a written root cause, such as a post-incident review, where the cause sat somewhere other than the service that alerted.
- Pin the time window to the incident, and take the answer from the review: the root cause, the mechanism that connected it to the symptom, and how it reached the alert. Never take it from the agent or the graph.
- `block_commands` stops both configurations reading the incident itself (`gcx irm`), so the agent has to work it out.
- Incident questions cost more per run (roughly $1 to $3) and vary more.

Please share these results especially. They tell us most about where the graph helps when it matters.

## Step 2: Check your graph's health

```bash
python3 $SKILL/kgkit/health.py my-questions.json --out results/
```

This runs gcx's own checks for the stack and for every service your questions name, then confirms each service is in the graph, in the right environment, with relationships.

- **Green:** go ahead.
- **Amber:** go ahead, and note the warnings. They often explain a miss later.
- **Red:** a service is missing or has no relationships. Fix that first with the `diagnose-entity-graph` skill (`gcx kg diagnose service <name>` prints suggested next steps), or pick another question. A test against a broken map measures the break, not the graph.

To check groups, add this to your questions file, using the label your graph filters on:

```json
"graph_groups": {"property": "<label, e.g. platform>", "env": "production", "values": ["<group a>", "<group b>"]}
```

The health check then reports, per group, how many services the graph holds and how many have any relationship. A group with services and no relationships is flagged amber. It runs two graph queries per group, so keep the list to the groups you care about.

## Step 3: Run the test

```bash
python3 $SKILL/kgkit/run.py my-questions.json --out results/ --dry-run
python3 $SKILL/kgkit/run.py my-questions.json --out results/pilot/ --only <one question id> --reps 1
python3 $SKILL/kgkit/run.py my-questions.json --out results/
```

The dry run prints the exact prompts. The pilot runs one question once on each side, so a setup problem costs one run rather than fifty; score it before the full run.

Both configurations use the same question, window and notes, with no line about sources. The with-graph prompt adds one line saying the graph is available through `gcx kg` and to start with it (`--graph-prompt first`, the default). That measures what the graph adds when the agent uses it. Two other settings answer different questions:

- `--graph-prompt available` says the graph is there and leaves the choice to the agent.
- `--graph-prompt none` says nothing, so it tests whether gcx's own help leads the agent to the graph. In our tests, agents given no hint didn't call the graph on simple questions.

An earlier version told the without-graph side to "answer from metrics, traces and logs only". Agents read that as a to-do list and ran extra trace queries, which made the graph look faster than it was, so the line is gone.

What the runner does for every run:

- Starts Claude Code in a new empty directory, with no plugins, skills, MCP servers, CLAUDE.md or memory, so nothing carries over between runs.
- Lets the agent run only gcx, through a small wrapper (`$SKILL/kgkit/bin/gcx`) that logs every command. In the without-graph configuration, the wrapper refuses `gcx kg`, `gcx assistant`, `gcx api`, queries on the graph's `asserts:` recording rules, the Knowledge Graph datasource, and the graph's resource types under `gcx resources`. Telemetry tools stay available in both, including `gcx appo11y services map`, which reads Tempo's service-graph metrics rather than the graph.
- Alternates which configuration goes first, and caps each run's spend (`--budget-usd`, default $3).
- Runs one at a time and waits 15 seconds after each (`--parallel 1`, `--pause 15`), so the test adds about one agent's worth of queries to your stack. Keep it that way on a production stack.
- Records a run whose agent process fails or times out as `failed`, so a wrong gcx version or a bad flag shows up instead of producing empty results.

Five runs per question per side is the default (`--reps`). With three, one odd run moves a question's score by a third.

## Step 4: Score

```bash
python3 $SKILL/kgkit/score.py my-questions.json --out results/
```

This reads every run's full transcript and the wrapper log, then:

- **Checks for leaks.** Any without-graph run that reached the graph, or ran anything other than gcx, is discarded and counted.
- **Scores each answer** against your key. Correct means every must-include item and no must-not-include item. Partly means some must-include items and none of the must-not-include. Wrong is anything else. A run with no answer block, and a run that failed, are reported separately.
- **Records tokens** for every run: input, output, cache writes and cache reads. The reports compare output tokens and fresh input (input plus cache writes). Cache reads dominate the totals and say little about how much work a run did, so they're shown in the full report but left out of the comparison.
- **Writes `results/summary.md`,** the short read:
  - a headline in plain words, about the questions where the agent actually used the graph
  - a summary table for those questions: fully correct runs, median time, output tokens and fresh input, with and without the graph, and the ratio
  - one row per question: what you asked, the size of the answer, a mark for every run on each side, median time and output tokens (without → with), and graph calls
  - where each side missed, with misses on questions where the graph wasn't called marked as run-to-run variation
  - caveats it works out from the results (sample size, questions both sides always got right, questions where the graph wasn't used, failed or discarded runs, graph health per group) and suggested next steps

`results/summary.md` names your questions and services, so check it before sending it outside your team. `results/report.md` has everything, including every run's answer. Keep it.

## Step 5: Share your scorecard (optional)

`results/scorecard.json` holds only question types, counts and numbers: no service names, queries or answers. To send it to Grafana:

```bash
python3 $SKILL/kgkit/share.py results/scorecard.json --questions my-questions.json --dry-run
python3 $SKILL/kgkit/share.py results/scorecard.json --questions my-questions.json \
  --agent "Claude Code 2.1, sonnet" --contact you@example.com --comments "what surprised you"
```

The script checks the scorecard holds only the fields `score.py` writes, checks that no service name or alias from your questions file appears anywhere in what it would send (including your comments), prints the exact payload, and sends only after you type `yes`. Contact and comments are optional; tell us especially if the graph didn't help. `--dry-run` never sends. An agent can pass `--yes` once it has shown you the dry-run output and you've agreed.

If you can't reach the form, email `results/scorecard.json` to your Grafana contact instead.

## Other agents

The automated runner uses Claude Code. With another agent, run the same two prompts (`run.py --dry-run` prints them) in fresh sessions:

- Put `$SKILL/kgkit/bin` first on PATH (run `chmod +x $SKILL/kgkit/bin/gcx` once), and set `KGKIT_REAL_GCX` to your real gcx, `KGKIT_MODE` to `with-graph` or `without-graph`, and `KGKIT_LOG` to a log file per run. The wrapper then blocks and logs exactly as above.
- Record time and tokens from your agent's usage display.
- Score the answers by hand against your key, and check each without-graph log for `blocked` lines.

## Known limits

- With five questions, the intervals are wide. Read the result per question, and as a direction for your stack, not a measurement.
- The wrapper blocks by command words. An agent determined to get round it could still find a way (for example, pointing an Infinity datasource query at a Grafana API), which is why the scorer also reads the transcripts.
- `gcx kg diagnose service` can report on a different instance of a service than the environment you asked for (seen on gcx 1.7.1). The health check catches this and checks the graph directly instead, so read its warnings.
- Your answer key is yours. If two people disagree about a dependency, so will the runs.
