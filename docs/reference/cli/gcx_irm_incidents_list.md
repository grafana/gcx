## gcx irm incidents list

List incidents.

### Synopsis

List incidents, most recent first.

--status and --severity are applied server-side. --labels and --from/--to are
applied client-side, one page at a time, so a highly selective --labels filter
can page through the full incident history before collecting --limit incidents.

--query is a raw query-string escape hatch and cannot be combined with the
structured --labels, --status, or --severity filters.

```
gcx irm incidents list [flags]
```

### Options

```
      --from string       Start of time range (RFC3339, unix timestamp, or relative e.g. now-7d)
  -h, --help              help for list
      --json string       Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --labels strings    Filter by label text or key:value, e.g. squad:mimir (may be repeated)
      --limit int         Maximum number of incidents to return (default 50)
  -o, --output string     Output format. One of: agents, json, table, wide, yaml (default "table")
      --query string      Raw incident query string, e.g. "isdrill:true"; cannot be combined with --labels, --status, or --severity
      --severity string   Filter by severity label, e.g. major (see: gcx irm incidents severities list)
      --status strings    Filter by status (active|resolved; repeatable, comma-separated)
      --to string         End of time range (RFC3339, unix timestamp, or relative e.g. now)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx irm incidents](gcx_irm_incidents.md)	 - Manage incidents.

