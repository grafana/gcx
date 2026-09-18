## gcx synthetic-monitoring checks test

Run a Synthetic Monitoring check once, without saving it.

### Synopsis

Run a Synthetic Monitoring check once against its probes without persisting
it — the check is never saved, and no schedule is created.

Results are recovered by polling Loki for the log lines the probes emit for
this execution (tagged type="adhoc"), the same mechanism the Synthetic
Monitoring app's "Test" button uses. Requires a Loki datasource containing SM
ad-hoc logs, and read access to it.

Note: ad-hoc test executions are billed the same as scheduled check
executions. See https://grafana.com/docs/grafana-cloud/cost-management-and-billing/manage-invoices/understand-your-invoice/synthetic-monitoring-invoice.md.

```
gcx synthetic-monitoring checks test [flags]
```

### Examples

```
  # Run a check once from a YAML file.
  gcx synthetic-monitoring checks test -f check.yaml

  # Specify the Loki datasource to poll for results.
  gcx synthetic-monitoring checks test -f check.yaml --logs-datasource-uid my-loki
```

### Options

```
  -f, --filename string              File containing the check manifest (YAML)
  -h, --help                         help for test
      --jq string                    jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string                  Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --logs-datasource-uid string   UID of the Loki datasource to poll for ad-hoc results
  -o, --output string                Output format. One of: agents, json, text, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx synthetic-monitoring checks](gcx_synthetic-monitoring_checks.md)	 - Manage Synthetic Monitoring checks.

