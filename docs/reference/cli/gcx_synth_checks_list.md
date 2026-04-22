## gcx synth checks list

List Synthetic Monitoring checks.

```
gcx synth checks list [flags]
```

### Examples

```
  # List all checks.
  gcx synth checks list

  # Filter by job glob.
  gcx synth checks list --job 'shopk8s-*'

  # Filter by label.
  gcx synth checks list --label env=prod
```

### Options

```
  -h, --help                help for list
      --job string          Filter by job name glob pattern (e.g. --job 'shopk8s-*')
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --label stringArray   Filter by label key=value (repeatable, e.g. --label env=prod)
  -o, --output string       Output format. One of: json, table, wide, yaml (default "table")
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

* [gcx synth checks](gcx_synth_checks.md)	 - Manage Synthetic Monitoring checks.

