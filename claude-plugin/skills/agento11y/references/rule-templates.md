# Rule Templates

Copy, fill in, and create with `gcx agento11y rules create -f rule.yaml`. Rule and evaluator IDs accept only letters, digits, `_`, and `.` — hyphens are rejected server-side. (`match` values like `agent_name` are names, not IDs, and keep whatever form the agent reports.)

> **`selector` is not one-size-fits-all — set it from the agent's shape.** Several templates below use `selector: user_visible_turn`, which only matches generations flagged as a user-facing turn. On a **multi-agent DAG / programmatic pipeline** or a **single-shot agent** those generations aren't user-visible turns, so a `user_visible_turn` rule matches nothing and scores zero — a live-looking but dead rule. For those agents use `selector: all_assistant_generations` (scoped with `match.agent_name`). Reserve `user_visible_turn` for genuine chat agents. Symptom of a wrong selector: the rule matches traffic but `eval_summary.total_scores` stays 0 (or `eval_summary` is absent).

## Basic — All User-Visible Turns

```yaml
rule_id: quality_check
enabled: true
selector: user_visible_turn
sample_rate: 1.0
evaluator_ids:
  - helpfulness_judge
  - basic_quality_gate
```

## Filtered — Target Specific Agents

```yaml
rule_id: claude_agent_quality
enabled: true
selector: user_visible_turn
match:
  agent_name:
    - my-agent
sample_rate: 1.0
evaluator_ids:
  - helpfulness_judge
```

## Sampled — Control Evaluation Cost

Sampling is conversation-level: all turns in a sampled conversation are evaluated.

```yaml
rule_id: sampled_toxicity_check
enabled: true
selector: all_assistant_generations
sample_rate: 0.1
evaluator_ids:
  - toxicity_judge
```

## Tool Call Evaluation

```yaml
rule_id: tool_quality
enabled: true
selector: tool_call_steps
match:
  agent_name:
    - my-agent
sample_rate: 1.0
evaluator_ids:
  - tool_correctness
```

## Sequential: Gate an LLM Judge Behind a Cheap Check

`execution_mode: sequential` runs `evaluator_ids` in order, and each evaluator must pass before the next one runs. The default, `parallel`, runs every evaluator independently. Put a deterministic evaluator first so the LLM judge only scores generations that pass it. Every evaluator except the last needs a pass condition on its outputs (`pass_threshold`, `pass_value` or `pass_match`). To switch a rule back, update it with `execution_mode: parallel`; leaving the field out keeps the current mode.

```yaml
rule_id: gated_helpfulness
enabled: true
selector: user_visible_turn
sample_rate: 0.1
execution_mode: sequential
evaluator_ids:
  - non_empty_response   # deterministic gate, runs first
  - helpfulness_judge    # llm_judge, runs only when the gate passes
```

## Tagged: Break Down Scores by Generation Tags

`filterable_tag_keys` lists up to 10 generation tag keys to expose as labels on evaluation score metrics: `team` becomes `agento11y_eval_tag_team`. The keys don't change which generations the rule matches. Keep the number of distinct tag values small to limit metric series.

```yaml
rule_id: quality_by_team
enabled: true
selector: all_assistant_generations
sample_rate: 0.1
evaluator_ids:
  - helpfulness_judge
filterable_tag_keys:
  - environment
  - team
```

## Model-Specific — Filter by Provider or Model

Match keys support glob patterns. Use to segment evaluation by the model that produced the generation — useful when comparing providers or rolling out evaluators only to specific model versions.

```yaml
rule_id: gpt4_grounding_check
enabled: true
selector: user_visible_turn
match:
  model.provider:
    - openai
  model.name:
    - gpt-4o*
sample_rate: 1.0
evaluator_ids:
  - grounding_judge
```
