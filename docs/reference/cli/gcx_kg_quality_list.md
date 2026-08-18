## gcx kg quality list

List entity quality reports, ranked by quality percent.

```
gcx kg quality list [flags]
```

### Examples

```
  gcx kg quality list --env <env>
  gcx kg quality list --env <env> --namespace <namespace> --sort asc
  gcx kg quality list --env <env> --failed-check span-metrics --failed-check service-graph-metrics
  gcx kg quality list --env <env> --json entityName,qualityPercent,failedCheckIds
```

### Options

```
      --entity string              Filter by entity name
      --env string                 Environment scope
      --failed-check stringArray   Only report entities with these failed check IDs; repeatable
  -h, --help                       help for list
      --jq string                  jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string                Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --namespace string           Namespace scope
  -o, --output string              Output format. One of: agents, json, table, yaml (default "table")
      --page int                   Page number (0-based)
      --page-size int              Page size (1-100) (default 25)
      --site string                Site scope
      --sort string                Sort by quality percent: asc (worst first) or desc (best first) (default "asc")
      --type string                Entity type to filter by (default "Service")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, CODEX_THREAD_ID, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx kg quality](gcx_kg_quality.md)	 - Inspect Knowledge Graph entity quality reports.

