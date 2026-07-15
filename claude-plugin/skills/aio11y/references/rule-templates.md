# Rule Templates

Copy, fill in, and create with `gcx aio11y rules create -f rule.yaml`. Rule and evaluator IDs accept only letters, digits, `_`, and `.` — hyphens are rejected server-side. (`match` values like `agent_name` are names, not IDs, and keep whatever form the agent reports.)

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
