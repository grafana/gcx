## gcx frontend errors get

Show one error group: stack frames, first-seen release, breakdowns, and recent occurrences.

### Synopsis

Show everything needed to fix one Frontend Observability error group.

<hash> is the decimal exceptionHash from gcx frontend errors list. The output
includes the type and message template, example messages, event, session, user
and page counts, a sparkline, the first-seen record from the Faro API (time,
git hash, bundle id; null when none exists), breakdowns by app version,
browser, page and OS, the most recent occurrences, and the longest recent stack
trace parsed into frames.

The sparkline has about 30 buckets of bucket_ms each, aligned to the Unix
epoch; sparkline.start is the start of the first bucket and can precede
range.from. When the group has no events in the window the command fails and
says whether the hash exists at all.

Each frame has function, file, line, col and in_app (false for node_modules,
browser extensions, native and <anonymous> frames). symbolicated is false when
no in-app frame has a function name and a line above 1, which usually means
source maps were not uploaded for the bundle.

The window defaults to the last 24 hours and cannot exceed 30 days. --sql adds
the PinotQL statements that ran.

```
gcx frontend errors get <hash> [flags]
```

### Examples

```
  # Everything about one error group
  gcx frontend errors get 13273781603667425932 --app checkout-web

  # Look further back, with more occurrences and the SQL
  gcx frontend errors get 13273781603667425932 --app 187 --since 7d --instances 10 --sql -o json
```

### Options

```
      --app string          Frontend Observability app: numeric ID, slug-id, or name (required)
  -d, --datasource string   Frontend Observability Pinot datasource UID (defaults to datasources.pinot in the context, then auto-discovery)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for get
      --instances int       Number of most recent occurrences to return (1-50) (default 3)
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --open                Open the primary query in Grafana Explore
  -o, --output string       Output format. One of: agents, json, text, yaml (default "text")
      --share-link          Print the Grafana Explore URL for the primary query to stderr
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --sql                 Include the PinotQL statements that ran in the output (runnable with gcx frontend query)
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
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

