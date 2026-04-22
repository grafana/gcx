# Rule Templates

Copy, fill in, and create with `gcx aio11y rules create -f rule.yaml`.

## Basic — All User-Visible Turns

```yaml
rule_id: quality-check
enabled: true
selector: user_visible_turn
sample_rate: 1.0
evaluator_ids:
  - helpfulness-judge
  - basic-quality-gate
```

## Filtered — Target Specific Agents

```yaml
rule_id: claude-agent-quality
enabled: true
selector: user_visible_turn
match:
  agent_name:
    - my-agent
sample_rate: 1.0
evaluator_ids:
  - helpfulness-judge
```

## Sampled — Control Evaluation Cost

Sampling is conversation-level: all turns in a sampled conversation are evaluated.

```yaml
rule_id: sampled-toxicity-check
enabled: true
selector: all_assistant_generations
sample_rate: 0.1
evaluator_ids:
  - toxicity-judge
```

## Tool Call Evaluation

```yaml
rule_id: tool-quality
enabled: true
selector: tool_call_steps
match:
  agent_name:
    - my-agent
sample_rate: 1.0
evaluator_ids:
  - tool-correctness
```

## Model-Specific — Filter by Provider or Model

Match keys support glob patterns. Use to segment evaluation by the model that produced the generation — useful when comparing providers or rolling out evaluators only to specific model versions.

```yaml
rule_id: gpt4-grounding-check
enabled: true
selector: user_visible_turn
match:
  model.provider:
    - openai
  model.name:
    - gpt-4o*
sample_rate: 1.0
evaluator_ids:
  - grounding-judge
```
