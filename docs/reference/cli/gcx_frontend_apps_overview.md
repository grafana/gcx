## gcx frontend apps overview

Show a Frontend Observability KPI snapshot for one app: page loads, errors, and Core Web Vitals.

### Synopsis

Show the headline KPIs for one Frontend Observability (Faro) app.

The argument is the app name or its numeric id; a name is resolved to its id
via the same app list used by "gcx frontend apps list". The snapshot mirrors
the Frontend Observability plugin's app overview page, computed from the Faro
RUM telemetry stored in Loki over --since (default 1h):

  - page loads and exception count (with errors as a percentage of loads)
  - the five Core Web Vitals at p75, each rated good / needs-improvement /
    poor against Google's thresholds (LCP, INP, CLS, FCP, TTFB)
  - the most frequent exceptions

Web Vitals use the same LogQL the plugin runs: a p75 over the Faro measurement
stream. The numbers require RUM data flowing to the stack's Loki; an app with
no telemetry in the window renders an empty snapshot and exits non-zero.

```
gcx frontend apps overview <app> [flags]
```

### Examples

```

  # KPI snapshot for an app by name over the last hour
  gcx frontend apps overview faro-shop-demo

  # By numeric id, last 15 minutes
  gcx frontend apps overview 153 --since 15m

  # JSON for scripting / agents
  gcx frontend apps overview faro-shop-demo -o json

  # Pin the Loki datasource
  gcx frontend apps overview faro-shop-demo -d grafanacloud-logs
```

### Options

```
  -d, --datasource string   Loki datasource UID (defaults to datasources.loki in config or auto-discovery)
  -h, --help                help for overview
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --since string        Lookback window for the KPI snapshot (e.g. 15m, 1h, 1d) — PromQL/LogQL duration syntax (default "1h")
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

* [gcx frontend apps](gcx_frontend_apps.md)	 - Manage Frontend Observability apps.

