## gcx logs adaptive drop-rules create

Create an adaptive log drop rule from a file.

### Synopsis

Create an adaptive log drop rule from a file.

The file's top-level "version" is the rule schema version (only 1 is supported). Omit it or set it to 1; do not confuse it with the rule revision in API responses.

```
gcx logs adaptive drop-rules create [flags]
```

### Options

```
  -f, --filename string   File containing the drop rule definition (use - for stdin)
  -h, --help              help for create
      --json string       Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string     Output format. One of: json, yaml (default "json")
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

* [gcx logs adaptive drop-rules](gcx_logs_adaptive_drop-rules.md)	 - Manage adaptive log drop rules.

