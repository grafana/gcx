## gcx synthetic-monitoring checks create

Create a Synthetic Monitoring check from a file.

### Synopsis

Create a Synthetic Monitoring check from a file.

Note: checks incur Grafana Cloud usage — each test execution is billed, and
check results are stored as metrics and logs, which count toward your metrics
and logs usage. See https://grafana.com/docs/grafana-cloud/cost-management-and-billing/manage-invoices/understand-your-invoice/synthetic-monitoring-invoice.md.

```
gcx synthetic-monitoring checks create [flags]
```

### Examples

```
  # Create a check from a YAML file.
  gcx synthetic-monitoring checks create -f check.yaml

  # Create and show resulting status.
  gcx synthetic-monitoring checks create -f check.yaml --show-status

  # Validate HTTP target before creating.
  gcx synthetic-monitoring checks create -f check.yaml --validate-targets
```

### Options

```
  -f, --filename string    File containing the check manifest (YAML)
  -h, --help               help for create
      --jq string          jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string        Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string      Output format. One of: agents, json, text, yaml (default "text")
      --show-status        Query and display check status after creation
      --validate-targets   Pre-flight HTTP HEAD request for HTTP check targets (warning only)
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

* [gcx synthetic-monitoring checks](gcx_synthetic-monitoring_checks.md)	 - Manage Synthetic Monitoring checks.

