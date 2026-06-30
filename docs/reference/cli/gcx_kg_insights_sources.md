## gcx kg insights sources

List the underlying metrics (name + label matchers) that source a specific insight.

```
gcx kg insights sources [Type--Name] [flags]
```

### Options

```
      --env string          Environment scope
  -f, --file string         Input file (YAML)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for sources
      --insight string      Insight name (e.g. LatencyAverageBreach, ResourceRateAnomaly) — sets the 'alertname' label
      --label stringArray   Assertion label as key=value (repeatable; typically copied from 'kg entities inspect' timeLines[].labels)
      --name string         Entity name
      --namespace string    Namespace scope
      --since string        Duration before --to (or now); mutually exclusive with --from/--to (e.g. 1h, 30m, 7d)
      --site string         Site scope
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
      --type string         Entity type
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

* [gcx kg insights](gcx_kg_insights.md)	 - Fetch chart data and source metrics for an active insight.

