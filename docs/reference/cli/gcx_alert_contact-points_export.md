## gcx alert contact-points export

Export all contact points in provisioning format.

```
gcx alert contact-points export [flags]
```

### Options

```
      --format string   Export format: yaml, json, or hcl (default "yaml")
  -h, --help            help for export
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx alert contact-points](gcx_alert_contact-points.md)	 - Manage Grafana alerting contact points.

