---
name: agento11y
description: >
  Inspects and manages Grafana Agent Observability resources via gcx:
  conversations, generations, evaluators, rules, scores, and templates.
  Use when the user wants to list or search conversations, inspect generations,
  manage evaluators (create, test, delete), set up evaluation rules, check scores,
  or browse evaluator templates. Trigger on phrases like "list conversations",
  "search generations", "what did the agent do", "debug LLM conversation",
  "create evaluator", "set up evaluation rule", "test evaluator", "check scores",
  "evaluate generation quality", or "set up online evaluation".
allowed-tools: Bash, Read, Write, Edit
---

# Agent Observability

Agent Observability records what LLM-powered applications do in production and scores the quality of their output.

Applications send generations (individual LLM API calls — request, response, model, tokens, tool calls) to Agent Observability. Generations belonging to the same user session are grouped into a conversation.

Evaluators are scoring functions (LLM judge, regex, heuristic, JSON schema, etc.) that assess generation quality. Rules bind evaluators to production traffic, they select which generations to evaluate (e.g. only user-visible turns), filter by agent/model, and control sampling rate. When a rule matches a generation, Agent Observability runs the bound evaluators and writes scores.

All commands live under `gcx agento11y`. Use `gcx agento11y <subcommand> --help` for flags and usage.

## Command Groups

| Group | Purpose |
|-------|---------|
| `conversations` | List, get, search conversations |
| `generations` | Get a single generation, list its scores |
| `agents` | List agents, get details, view versions |
| `evaluators` | List, get, upsert, delete, test evaluators |
| `rules` | List, get, create, update, delete evaluation rules |
| `templates` | List, get built-in evaluator templates |
| `judge` | List judge providers and models |
| `experiments` | List, get, create, update, cancel runs; inspect scores and reports |

Delete commands (`evaluators delete`, `rules delete`) require `--force` to skip confirmation in agent mode (there is no `-f` shorthand on delete). List first to confirm the target ID:

```bash
gcx agento11y evaluators list
gcx agento11y evaluators delete <id> --force

gcx agento11y rules list
gcx agento11y rules delete <id> --force
```

Deleting an evaluator referenced by a rule may leave the rule pointing at a missing evaluator — check `gcx agento11y rules list` after.

## Conversation Search

Defaults to last 24 hours. Filter syntax: `key operator "value"`, space-separated.

```bash
gcx agento11y conversations search --filters 'agent = "my-agent" status = "error"'
gcx agento11y conversations search --filters 'agent = "my-agent"' --from 2026-04-01T00:00:00Z --to 2026-04-14T00:00:00Z
```

**Filter keys:** `model`, `provider`, `agent`, `agent.version`, `status`, `error.type`, `error.category`, `duration`, `tool.name`, `operation`, `namespace`, `cluster`, `service`, `generation_count`, `eval.passed`, `eval.evaluator_id`, `eval.score_key`, `eval.score`

**Operators:** `=`, `!=`, `>`, `<`, `>=`, `<=`, `=~` (regex)

## Evaluator Kind Decision Table

| User describes | Kind |
|----------------|------|
| "check if response is helpful / toxic / grounded" | `llm_judge` |
| "combined quality score with explanation" | `llm_judge` |
| "validate JSON output format" | `json_schema` |
| "check if response contains / doesn't contain X" | `regex` |
| "response must be non-empty and at least N chars" | `heuristic` |
| "check multiple conditions (non-empty AND has greeting)" | `heuristic` |

Copy-paste definitions for each kind, with the constraints the API enforces for each, are in [references/evaluator-examples.md](references/evaluator-examples.md).

## Input Format

`gcx agento11y evaluators get -o yaml` and `gcx agento11y rules get -o yaml` emit K8s-style manifests (`apiVersion/kind/metadata/spec`). `evaluators upsert -f`, `rules create -f`, and `rules update -f` expect top-level fields only. Do not round-trip get output into create/update.

IDs (`evaluator_id`, `rule_id`) accept only letters, digits, `_`, and `.` — hyphens are rejected server-side. `version` is required on evaluator definitions — it versions the evaluator itself, separate from any schema version inside `config` — see [references/evaluator-examples.md](references/evaluator-examples.md) for full examples of every kind.

Rule definition:

```yaml
rule_id: my_rule
enabled: true
selector: user_visible_turn
sample_rate: 1.0
evaluator_ids:
  - my_evaluator
match:
  agent_name:
    - my-agent
```

There is no `evaluators update` command; to change an evaluator, re-run `upsert` with the same `evaluator_id` and a new `version` (re-using an existing version is rejected with a 409).

## Setting Up Online Evaluation

1. Pick a template: `gcx agento11y templates list`, then `gcx agento11y templates get <id> -o yaml`. Template output includes `kind`, `config`, and `output_keys` — copy these into a new evaluator definition and add your own `evaluator_id`. Do not pass the template output directly to `evaluators upsert`.
2. Write an evaluator YAML using the input format above, create: `gcx agento11y evaluators upsert -f evaluator.yaml`
3. Test against a real generation: `gcx agento11y evaluators test -e <evaluator-id> -g <generation-id>`
4. Iterate until the evaluator scores as expected
5. Write a rule YAML (see [rule-templates.md](references/rule-templates.md) for copy-paste templates), create: `gcx agento11y rules create -f rule.yaml`
6. Verify: `gcx agento11y rules list`

## Rule Selectors

| Selector | What it evaluates |
|----------|-------------------|
| `user_visible_turn` | Final assistant generation visible to the user |
| `all_assistant_generations` | Every assistant generation in the conversation |
| `tool_call_steps` | Tool call generations |

## Rule Match Keys

All values are arrays. Glob-capable keys support `*`, `?`, `[...]` patterns.

| Key | Glob | Description |
|-----|------|-------------|
| `agent_name` | yes | Agent name |
| `agent_version` | yes | Agent version string |
| `operation_name` | yes | Operation name |
| `model.provider` | yes | Model provider (e.g. `openai`, `anthropic`) |
| `model.name` | yes | Model name (e.g. `gpt-4o`, `claude-sonnet-4-5-20250514`) |
| `mode` | no | `SYNC` or `STREAM` |
| `error.type` | no | Error type (also accepts `present`/`absent`) |
| `error.category` | no | Error category (also accepts `present`/`absent`) |
| `tags.<key>` | no | Custom tag value (e.g. `tags.env`) |
