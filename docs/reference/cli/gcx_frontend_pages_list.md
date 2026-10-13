## gcx frontend pages list

List pages with Web Vitals p75, page loads, and error counts.

### Synopsis

List the pages (pageId) of a Frontend Observability app with page loads, error
counts, and the p75 of each Core Web Vital (LCP, INP, CLS, FCP, TTFB).

Each p75 carries a rating from the web.dev thresholds: good, needs-improvement,
or poor (LCP 2500/4000 ms, INP 200/500 ms, CLS 0.1/0.25, FCP 1800/3000 ms,
TTFB 800/1800 ms). A vital with no reports in the window has a null p75 and a
null rating, and sorts last. The filter flags match stored values exactly.

next suggests errors list for the listed page with the most errors, and is
omitted when none has any. The window defaults to the last 24 hours and cannot
exceed 30 days. --sql adds the PinotQL statements that ran.

```
gcx frontend pages list [flags]
```

### Examples

```
  # Slowest pages by LCP
  gcx frontend pages list --app checkout-web

  # Pages with the most errors in one release
  gcx frontend pages list --app 187 --version 4.18.0 --sort errors

  # One page, with the SQL that ran
  gcx frontend pages list --app 187 --page /cart --sql -o json
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
      --limit int            Maximum number of pages to return (1-200) (default 20)
      --open                 Open the primary query in Grafana Explore
      --os string            Only this operating system: the exact osName as stored, e.g. "Mac OS" (copy from errors get breakdown.os)
  -o, --output string        Output format. One of: agents, json, table, yaml (default "table")
      --page string          Only this page: the exact pageId as stored, e.g. /d/*/home (copy from pages list or errors get breakdown.page), not a URL
      --share-link           Print the Grafana Explore URL for the primary query to stderr
      --since string         Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --sort string          Sort order, descending: lcp, inp, cls, fcp, ttfb, loads, errors (missing vitals last) (default "lcp")
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

* [gcx frontend pages](gcx_frontend_pages.md)	 - Inspect Frontend Observability page performance.

