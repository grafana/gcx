## gcx frontend errors list

List error groups for a Frontend Observability app.

### Synopsis

List error groups (exceptionHash + exceptionType) for a Frontend Observability
app from Pinot, joined with the first-seen record (time, git hash, bundle) the
Faro API keeps for each group.

Each group reports its event count, affected sessions and pages, a trend that
compares the first and second half of the window (up/down when the change is
over 20%), and when it was last seen. Groups the Faro API has no first-seen
record for show first_seen: null.

Pinot returns at most 1000 groups, ordered by count. total_groups counts the
groups returned, after --new-since; groups_capped is true when the cap was
hit, in which case rare groups may be missing and a filter flag or a shorter
window narrows the set. The filter flags match stored values exactly.

--new-since keeps only groups first seen at or after a time, which answers
"what broke since my deploy". --sort newest orders by first seen.

The window defaults to the last 24 hours and cannot exceed 30 days (Pinot
retention). --sql adds the PinotQL that ran; each statement runs unchanged
with gcx frontend query.

```
gcx frontend errors list [flags]
```

### Examples

```
  # Top errors in the last 24 hours
  gcx frontend errors list --app checkout-web

  # What broke since a deploy
  gcx frontend errors list --app checkout-web --new-since 2026-10-07T09:00:00Z

  # Errors on one page in one release, with the SQL that ran
  gcx frontend errors list --app 187 --page /cart --version 4.18.0 --sql
```

### Options

```
      --app string           Frontend Observability app: numeric ID, slug-id, or name (required)
      --browser string       Only this browser: the exact browserName as stored, e.g. Chrome (copy from errors get breakdown.browser)
  -d, --datasource string    Frontend Observability Pinot datasource UID (defaults to datasources.pinot in the context, then auto-discovery)
      --environment string   Only this app environment: the exact appEnvironment as stored, e.g. production
      --from string          Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                 help for list
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int            Maximum number of error groups to return (1-200) (default 20)
      --new-since string     Only error groups first seen at or after this time (RFC3339, Unix timestamp, relative like now-6h, or a duration like 6h)
      --open                 Open the primary query in Grafana Explore
      --os string            Only this operating system: the exact osName as stored, e.g. "Mac OS" (copy from errors get breakdown.os)
  -o, --output string        Output format. One of: agents, json, table, wide, yaml (default "table")
      --page string          Only this page: the exact pageId as stored, e.g. /d/*/home (copy from pages list or errors get breakdown.page), not a URL
      --share-link           Print the Grafana Explore URL for the primary query to stderr
      --since string         Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --sort string          Sort order: count, sessions, or newest (first seen, unknown last) (default "count")
      --sql                  Include the PinotQL statements that ran in the output (runnable with gcx frontend query)
      --to string            End time (RFC3339, Unix timestamp, or relative like 'now')
      --version string       Only this app version: the exact appVersion as stored (copy from errors get breakdown.app_version)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx frontend errors](gcx_frontend_errors.md)	 - Triage Frontend Observability error groups.

