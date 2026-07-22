# Evaluator Definition Examples

Copy-paste definitions for `evaluators upsert -f`, one per kind, with the
constraints the API enforces for that kind. Shared rules for every kind:

- `evaluator_id` accepts only letters, digits, `_`, and `.` (hyphens are
  rejected server-side).
- `version` is required at the top level. It versions the evaluator itself —
  distinct from `config.version`, which (for heuristic) selects the config
  schema. To change an evaluator later, re-run `upsert` with the same
  `evaluator_id` and a new `version`; re-using an existing version is a 409.
- Test any evaluator without persisting results:
  `gcx agento11y evaluators test -e <evaluator-id> -g <generation-id>`

## Regex

Matches a Go regular expression against the assistant's reply. Regex
evaluators require **bool** output keys (a numeric `type` is rejected), and
`pass_value` is only valid on bool keys. Use inline flags like `(?i)` for
case-insensitive matching.

```yaml
evaluator_id: contains_apology
kind: regex
version: "1"
description: "Reply contains the word sorry (case-insensitive)"
config:
  pattern: "(?i)sorry"
output_keys:
  - key: contains_apology
    type: bool
    pass_value: true       # true = matching counts as pass
```

## Heuristic

Deterministic server-side checks (no LLM call), combined in a rule group.
The config needs `version: v2` and a `root` group; multiple rules under one
group are ANDed/ORed via `operator`.

```yaml
evaluator_id: reply_min_length
kind: heuristic
version: "1"
description: "Reply is non-empty and at least 20 characters"
config:
  version: v2
  root:
    kind: group
    operator: and
    rules:
      - kind: rule
        type: not_empty
      - kind: rule
        type: min_length
        value: 20
output_keys:
  - key: reply_min_length
    type: bool
    pass_value: true
```

Browse `gcx agento11y templates list` / `templates get <id> -o yaml` for the
built-in heuristic rule types (`not_empty`, `min_length`, …) and more config
shapes — but copy fields into your own definition; template output is not
directly valid `upsert` input.

## LLM judge

Scores via a configured judge model. The `user_prompt` must inject the
content being judged with template variables — `{{assistant_response}}` for
the reply, `{{latest_user_message}}` for the user turn (the convention the
built-in `template.*` judge templates use); without them the judge never sees
the generation. Pick `provider`/`model` from what the stack actually has:

```bash
gcx agento11y judge list-providers
gcx agento11y judge list-models --provider <provider-id>
```

Numeric output keys with ranges and a pass threshold are the norm here.

```yaml
evaluator_id: helpfulness_judge
kind: llm_judge
version: "1"
description: "Rates helpfulness of the reply 1-10"
config:
  provider: <provider-id>   # from `judge list-providers`
  model: <model-id>         # from `judge list-models --provider <provider-id>`
  system_prompt: "You rate how helpful an assistant reply is."
  user_prompt: |-
    Latest user message:
    {{latest_user_message}}

    Assistant response:
    {{assistant_response}}

    Rate the helpfulness of the assistant response from 1 to 10.
  temperature: 0
  max_tokens: 256
output_keys:
  - key: score
    type: number
    min: 1
    max: 10
    pass_threshold: 4
```
