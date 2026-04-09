## gcx sigil evaluators test

Run an evaluator against a generation without persisting results.

```
gcx sigil evaluators test [flags]
```

### Examples

```
  # Test an existing evaluator against a generation.
  gcx sigil evaluators test -e my-evaluator -g gen-abc123

  # Test from a full request file (kind, config, output_keys, generation_id).
  gcx sigil evaluators test -f test-request.yaml

  # Test with JSON output.
  gcx sigil evaluators test -e my-evaluator -g gen-abc123 -o json
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
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx sigil evaluators](gcx_sigil_evaluators.md)	 - Manage evaluator definitions (LLM judge, regex, heuristic).

