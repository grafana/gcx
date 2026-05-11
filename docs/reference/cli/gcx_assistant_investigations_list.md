## gcx assistant investigations list

List investigations.

### Synopsis

List investigations. Auto-detects whether the stack supports Lodestone (v2) and uses the richer endpoint when available.

```
gcx assistant investigations list [flags]
```

### Options

```
      --from string      Lower bound on creation time, RFC3339 (Lodestone only)
  -h, --help             help for list
      --include-legacy   Include legacy (pre-Lodestone) investigations (Lodestone only) (default true)
      --json string      Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --label string     Filter by label, key:value format (Lodestone only)
      --limit int        Maximum number of investigations to return (default 50)
      --offset int       Number of investigations to skip (for pagination)
      --order string     Sort order: asc|desc (Lodestone only)
  -o, --output string    Output format. One of: agents, json, table, wide, yaml (default "table")
      --q string         Search text across title, description, chat name (Lodestone only)
      --scope string     Visibility scope: all|mine|teams|system (Lodestone only)
      --sort string      Sort field: createdAt|updatedAt|title|state (Lodestone only)
      --state string     Filter by investigation state (comma-separated, or "all")
      --team string      Filter to a specific team (Lodestone only)
      --to string        Upper bound on creation time, RFC3339 (Lodestone only)
      --view string      Result detail level: full|lite (Lodestone only)
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

* [gcx assistant investigations](gcx_assistant_investigations.md)	 - Manage Grafana Assistant investigations.

