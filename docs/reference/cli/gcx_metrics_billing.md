## gcx metrics billing

Query Grafana Cloud billing metrics (grafanacloud_*)

### Synopsis

Query Grafana Cloud billing metrics via the grafanacloud-usage datasource
that ships pre-provisioned on every Grafana Cloud stack.

These commands are thin conveniences over the generic metrics subcommands;
the --datasource flag defaults to "grafanacloud-usage" but can be overridden.

### Options

```
  -h, --help   help for billing
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx metrics](gcx_metrics.md)	 - Query Prometheus datasources and manage Adaptive Metrics
* [gcx metrics billing labels](gcx_metrics_billing_labels.md)	 - List label names available on billing metrics
* [gcx metrics billing query](gcx_metrics_billing_query.md)	 - Execute a PromQL query against billing metrics
* [gcx metrics billing series](gcx_metrics_billing_series.md)	 - List billing time series matching a selector

