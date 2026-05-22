## gcx irm oncall alert-groups list

List alert groups.

### Synopsis

List alert groups.

By default, lists root alert groups (excluding child groups merged into parents) in
firing, acknowledged, or silenced state. Resolved groups are excluded.

Use --all to bypass these defaults entirely (returns resolved and child groups too).
Use --state to override the status filter (e.g. --state firing,acknowledged).
Use --include-child-groups to keep the status default but include child groups.

```
gcx irm oncall alert-groups list [flags]
```

### Options

```
      --all                    Bypass the default status and is_root filters (returns resolved groups and child groups too)
      --has-related-incident   Limit to alert groups linked to an incident
  -h, --help                   help for list
      --include-child-groups   Include child groups (drops the is_root filter while keeping the status default)
      --integration strings    Filter by integration PK (repeatable, comma-separated)
      --json string            Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int              Maximum number of alert groups to return (0 for all, capped by an internal safety limit) (default 50)
      --max-age string         Exclude groups older than this duration (e.g. 1h, 24h, 7d)
      --mine                   Limit to alert groups for the authenticated user
  -o, --output string          Output format. One of: agents, json, table, wide, yaml (default "table")
      --state strings          Filter by state (firing|acknowledged|resolved|silenced; repeatable, comma-separated). Default: firing,acknowledged,silenced
      --team strings           Filter by team PK (repeatable, comma-separated)
      --with-resolution-note   Limit to alert groups that have a resolution note
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx irm oncall alert-groups](gcx_irm_oncall_alert-groups.md)	 - Manage alert groups.

