## gcx irm oncall alert-groups list

List alert groups.

### Synopsis

List alert groups.

By default, lists root alert groups (excluding child groups merged into parents) in
firing, acknowledged, or silenced state. Resolved groups are excluded.

Use --all to bypass these defaults entirely (returns resolved and child groups too).
Use --state to override the status filter (e.g. --state firing,acknowledged).
Use --include-child-groups to keep the status default but include child groups.

Alert group records carry no escalation chain field, so --escalation-chain is the
only way to attribute alert load to the rotation that was actually paged. It is not
interchangeable with --integration: one integration routes to several chains.

--max-age anchors to now; use --from/--to for a historical started-at window, and
--resolved-from/--resolved-to for a resolved-at window. They accept RFC3339, a unix
timestamp, or a relative expression like now-30d. --max-age and --from/--to cannot
be combined.

```
gcx irm oncall alert-groups list [flags]
```

### Examples

```
  # List firing, acknowledged, and silenced root groups
  gcx irm oncall alert-groups list

  # Narrow to one team, most recent day
  gcx irm oncall alert-groups list --team <team-id> --max-age 24h

  # Attribute load to a rotation (chain IDs: gcx irm oncall escalation-chains list)
  gcx irm oncall alert-groups list --escalation-chain <chain-id> --all

  # Historical window, including resolved groups
  gcx irm oncall alert-groups list --from now-30d --to now-7d --all

  # Groups resolved last week by a given user
  gcx irm oncall alert-groups list --resolved-by <user-id> --resolved-from now-7d --all
```

### Options

```
      --acknowledged-by strings    Filter by acknowledging user ID (repeatable, comma-separated; see: gcx irm oncall users list)
      --all                        Bypass the default status and is_root filters (returns resolved groups and child groups too)
      --escalation-chain strings   Filter by escalation chain ID (repeatable, comma-separated; see: gcx irm oncall escalation-chains list)
      --from string                Start of the started-at window (RFC3339, unix timestamp, or relative e.g. now-30d); cannot be combined with --max-age
      --has-related-incident       Limit to alert groups linked to an incident
  -h, --help                       help for list
      --include-child-groups       Include child groups (drops the is_root filter while keeping the status default)
      --integration strings        Filter by integration PK (repeatable, comma-separated)
      --jq string                  jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string                Comma-separated list of dotted field paths to include in JSON output (e.g. spec.name), or 'list' (or '?') to discover the available paths
      --limit int                  Maximum number of alert groups to return (0 for all, capped by an internal safety limit) (default 50)
      --max-age string             Exclude groups older than this duration (e.g. 1h, 24h, 7d)
      --mine                       Limit to alert groups for the authenticated user
  -o, --output string              Output format. One of: agents, json, table, wide, yaml (default "table")
      --resolved-by strings        Filter by resolving user ID (repeatable, comma-separated; see: gcx irm oncall users list)
      --resolved-from string       Start of the resolved-at window (RFC3339, unix timestamp, or relative e.g. now-30d)
      --resolved-to string         End of the resolved-at window (RFC3339, unix timestamp, or relative e.g. now-7d); defaults to now
      --state strings              Filter by state (firing|acknowledged|resolved|silenced; repeatable, comma-separated). Default: firing,acknowledged,silenced
      --team strings               Filter by team PK (repeatable, comma-separated)
      --to string                  End of the started-at window (RFC3339, unix timestamp, or relative e.g. now-7d); defaults to now
      --with-resolution-note       Limit to alert groups that have a resolution note
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

* [gcx irm oncall alert-groups](gcx_irm_oncall_alert-groups.md)	 - Manage alert groups.

