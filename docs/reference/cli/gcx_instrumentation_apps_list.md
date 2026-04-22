## gcx instrumentation apps list

List instrumentation app configurations.

### Synopsis

List instrumentation app configurations.

Without --cluster, fans out one API call per connected cluster (bounded concurrency, default 10). For large deployments with many clusters, this may take several seconds.

```
gcx instrumentation apps list [flags]
```

### Options

```
      --cluster string   Filter by cluster name (direct call, skips fan-out)
  -h, --help             help for list
      --json string      Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int        Maximum number of items to return (0 for unlimited) (default 50)
  -o, --output string    Output format. One of: json, table, wide, yaml (default "table")
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

* [gcx instrumentation apps](gcx_instrumentation_apps.md)	 - Manage instrumentation app configurations.

