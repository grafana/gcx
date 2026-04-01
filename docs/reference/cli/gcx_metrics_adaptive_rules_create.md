## gcx metrics adaptive rules create

Create an aggregation rule.

```
gcx metrics adaptive rules create [flags]
```

### Options

```
      --aggregation-delay string      Aggregation delay (e.g. 5m)
      --aggregation-interval string   Aggregation interval (e.g. 1m)
      --aggregations strings          Aggregation types: sum, count, min, max, sum:counter (comma-separated)
      --drop                          Drop the metric entirely
      --drop-labels strings           Labels to drop (comma-separated)
  -h, --help                          help for create
      --json string                   Comma-separated list of fields to include in JSON output, or '?' to discover available fields
      --keep-labels strings           Labels to keep (comma-separated)
      --match-type string             Match type: exact, prefix, or suffix (default "exact")
      --metric string                 Metric name (required)
  -o, --output string                 Output format. One of: json, yaml (default "json")
      --segment string                Segment ID
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx metrics adaptive rules](gcx_metrics_adaptive_rules.md)	 - Manage aggregation rules.

