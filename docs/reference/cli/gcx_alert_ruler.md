## gcx alert ruler

Manage datasource-managed (Mimir/Loki ruler) rule groups.

### Synopsis

Manage alerting and recording rules stored in a Mimir or Loki ruler,
via Grafana's per-datasource ruler proxy.

These are datasource-managed rules, distinct from Grafana-managed alert rules.
Grafana-managed rules are read via 'gcx alert rules' and written via
'gcx resources pull/push alertrules' — the write path requires Grafana 13+,
where the rules.alerting.grafana.app API is enabled by default (on Grafana 12
it must be enabled explicitly via feature toggle). Every ruler command
requires --datasource with the UID of a Prometheus-flavored or Loki datasource.

### Options

```
  -h, --help   help for ruler
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

* [gcx alert](gcx_alert.md)	 - Manage Grafana alert rules and alert groups
* [gcx alert ruler groups](gcx_alert_ruler_groups.md)	 - Manage ruler rule groups.
* [gcx alert ruler namespaces](gcx_alert_ruler_namespaces.md)	 - Manage ruler namespaces.

