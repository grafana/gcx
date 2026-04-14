---
name: sigil
description: >
  Use when the user wants to list or search Sigil conversations, inspect generations,
  manage evaluators (create, test, delete), set up evaluation rules, check scores,
  or browse evaluator templates. Trigger on phrases like "list conversations",
  "search generations", "what did the agent do", "debug LLM conversation",
  "create evaluator", "set up evaluation rule", "test evaluator", "check scores",
  "evaluate generation quality", or "set up online evaluation".
allowed-tools: [gcx, Bash, Read, Write, Edit]
---

# Sigil — AI Observability

Sigil is Grafana's AI observability platform. It records what LLM-powered applications do in production and scores the quality of their output.

Applications send generations (individual LLM API calls — request, response, model, tokens, tool calls) to Sigil. Generations belonging to the same user session are grouped into a conversation.

Evaluators are scoring functions (LLM judge, regex, heuristic, JSON schema, etc.) that assess generation quality. Rules bind evaluators to production traffic, they select which generations to evaluate (e.g. only user-visible turns), filter by agent/model, and control sampling rate. When a rule matches a generation, Sigil runs the bound evaluators and writes scores.

All commands live under `gcx sigil`. Use `gcx sigil <subcommand> --help` for flags and usage.

## Command Groups

| Group | Purpose |
|-------|---------|
| `conversations` | List, get, search conversations |
| `generations` | Get a single generation |
| `agents` | List agents, get details, view versions |
| `evaluators` | List, get, create, delete, test evaluators |
| `rules` | List, get, create, update, delete evaluation rules |
| `templates` | List, get built-in evaluator templates |
| `scores` | List scores for a generation |
| `judge` | List judge providers and models |

Delete commands (`evaluators delete`, `rules delete`) require `-f` to skip confirmation in agent mode. List first to confirm the target ID:

```bash
gcx sigil evaluators list
gcx sigil evaluators delete <id> -f

gcx sigil rules list
gcx sigil rules delete <id> -f
```

Deleting an evaluator referenced by a rule may leave the rule pointing at a missing evaluator — check `gcx sigil rules list` after.

## Conversation Search

Defaults to last 24 hours. Filter syntax: `key operator "value"`, space-separated.

```bash
gcx sigil conversations search --filters 'agent = "my-agent" status = "error"'
gcx sigil conversations search --filters 'agent = "my-agent"' --from 2026-04-01T00:00:00Z --to 2026-04-14T00:00:00Z
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

## Input Format

`gcx sigil evaluators get -o yaml` and `gcx sigil rules get -o yaml` emit K8s-style manifests (`apiVersion/kind/metadata/spec`). The `create -f` and `update -f` commands expect top-level fields only. Do not round-trip get output into create/update.

Evaluator definition:

```yaml
evaluator_id: my-evaluator
kind: llm_judge
description: "..."
config:
  provider: openai
  model: gpt-4o
  system_prompt: "..."
  user_prompt: "..."
  temperature: 0
  max_tokens: 256
output_keys:
  - key: score
    type: number
    min: 1
    max: 10
    pass_threshold: 4
```

Rule definition:

```yaml
rule_id: my-rule
enabled: true
selector: user_visible_turn
sample_rate: 1.0
evaluator_ids:
  - my-evaluator
match:
  agent_name:
    - my-agent
```

Evaluators use create-or-update semantics: re-creating with the same `evaluator_id` updates it.

## Setting Up Online Evaluation

1. Pick a template: `gcx sigil templates list`, then `gcx sigil templates get <id> -o yaml`. Template output includes `kind`, `config`, and `output_keys` — copy these into a new evaluator definition and add your own `evaluator_id`. Do not pass the template output directly to `evaluators create`.
2. Write an evaluator YAML using the input format above, create: `gcx sigil evaluators create -f evaluator.yaml`
3. Test against a real generation: `gcx sigil evaluators test -e <evaluator-id> -g <generation-id>`
4. Iterate until the evaluator scores as expected
5. Write a rule YAML (see `references/rule-templates.md`), create: `gcx sigil rules create -f rule.yaml`
6. Verify: `gcx sigil rules list`

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
