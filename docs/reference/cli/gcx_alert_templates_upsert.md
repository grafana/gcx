## gcx alert templates upsert

Create or update a notification template.

### Synopsis

Create or update a notification template.

The provisioning API uses a single PUT endpoint keyed by template name,
so the same command handles both create and update.

```
gcx alert templates upsert [flags]
```

### Options

```
  -f, --filename string   File containing the template definition (JSON/YAML, use - for stdin)
  -h, --help              help for upsert
      --json string       Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string     Output format. One of: json, yaml (default "json")
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

* [gcx alert templates](gcx_alert_templates.md)	 - Manage Grafana alerting notification templates.

