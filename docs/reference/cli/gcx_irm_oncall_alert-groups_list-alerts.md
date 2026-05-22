## gcx irm oncall alert-groups list-alerts

List individual alerts for an alert group.

```
gcx irm oncall alert-groups list-alerts <alert-group-id> [flags]
```

### Options

```
  -h, --help            help for list-alerts
      --history         Opt out of collapse: emit every stored Alert as its own row with status.occurrences=1 (default behaviour collapses re-fires by alert label set)
      --include-raw     Include the unprocessed Alertmanager-shape payload under status.raw on each alert (hidden by default; status.{dimensions,links,...} are the promoted view of the same data)
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int       Cap on number of alerts retrieved (0 = no cap) (default 100)
  -o, --output string   Output format. One of: agents, json, table, wide, yaml (default "table")
      --slim            Skip per-alert retrieval; emit only metadata + alert-group back-pointer
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

