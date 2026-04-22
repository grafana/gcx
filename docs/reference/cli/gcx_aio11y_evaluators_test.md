## gcx aio11y evaluators test

Run an evaluator against a generation without persisting results.

```
gcx aio11y evaluators test [flags]
```

### Examples

```
  # Test an existing evaluator against a generation.
  gcx aio11y evaluators test -e my-evaluator -g gen-abc123

  # Test from a full request file (kind, config, output_keys, generation_id).
  gcx aio11y evaluators test -f test-request.yaml

  # Test with JSON output.
  gcx aio11y evaluators test -e my-evaluator -g gen-abc123 -o json
```

### Options

```
      --conversation-id string   Conversation ID hint for generation lookup
  -e, --evaluator string         Evaluator ID to test (fetches config from server)
  -f, --filename string          File with full eval:test request body (use - for stdin)
  -g, --generation string        Generation ID to evaluate
  -h, --help                     help for test
      --json string              Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string            Output format. One of: json, table, yaml (default "table")
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx aio11y evaluators](gcx_aio11y_evaluators.md)	 - Manage evaluator definitions (LLM judge, regex, heuristic).

