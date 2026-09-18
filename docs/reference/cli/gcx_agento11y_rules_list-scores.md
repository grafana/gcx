## gcx agento11y rules list-scores

List online evaluation scores for a rule.

### Synopsis

List online evaluation score rows for a rule.

Each row may include an LLM-judge explanation in the payload. The default table
shows score key, value, pass/fail, evaluator, and timestamp only; use -o wide
(truncated explanation column) or -o json for full explanation text.

Use --passed=false to focus on failing scores for failure theme analysis.
Filter by evaluator, time range, agent, model, or provider as needed.

```
gcx agento11y rules list-scores <rule-id> [flags]
```

### Examples

```
  # Recent scores (summary table; no explanations).
  gcx agento11y rules list-scores <rule-id>

  # Failing scores with explanations.
  gcx agento11y rules list-scores <rule-id> --passed=false -o json

  # Wide table with truncated explanation column.
  gcx agento11y rules list-scores <rule-id> --passed=false -o wide

  # Scoped to one evaluator and time window.
  gcx agento11y rules list-scores <rule-id> --evaluator-id <id> --from 2026-04-01T00:00:00Z --to 2026-04-02T00:00:00Z -o json
```

### Options

```
      --agent-name stringArray    Filter by exact agent name, case-sensitive (repeat to OR; not comma-split)
      --evaluator-id string       Filter by evaluator ID
      --from string               Inclusive lower bound on created_at (RFC3339)
  -h, --help                      help for list-scores
      --jq string                 jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string               Comma-separated list of dotted field paths to include in JSON output (e.g. spec.name), or 'list' (or '?') to discover the available paths
      --limit int                 Maximum number of scores to return. 0, or a value above 1000, returns up to the 1000-row safety cap; narrow with filters to see beyond it (default 100)
      --max-value float           Inclusive upper bound on numeric score value (omit for no upper bound)
      --min-value float           Inclusive lower bound on numeric score value (omit for no lower bound)
      --model stringArray         Filter by exact generation model, case-sensitive (repeat to OR; not comma-split)
  -o, --output string             Output format. One of: agents, json, table, wide, yaml (default "table")
      --passed                    Filter by pass/fail (omit for all scores)
      --provider stringArray      Filter by exact generation provider, case-sensitive (repeat to OR; not comma-split)
      --score-value stringArray   Filter by exact score value, case-sensitive (repeat to OR; not comma-split, max 20)
      --sort-by string            Sort field: created_at or value (default "created_at")
      --sort-dir string           Sort direction: asc or desc (default "desc")
      --to string                 Exclusive upper bound on created_at (RFC3339)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx agento11y rules](gcx_agento11y_rules.md)	 - Manage rules that route generations to evaluators.

