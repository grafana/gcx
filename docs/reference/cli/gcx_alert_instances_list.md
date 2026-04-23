## gcx alert instances list

List alert instances.

```
gcx alert instances list [flags]
```

### Options

```
      --datasource string   Datasource UID to query (default: Grafana-managed rules)
      --folder string       Filter by folder UID
      --group string        Filter by group name
  -h, --help                help for list
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --name string         Filter by rule name (regex, e.g. 'Tempo.*')
  -o, --output string       Output format. One of: json, table, wide, yaml (default "table")
      --rule string         Filter by rule UID
      --state string        Filter by alert instance state (firing, pending, inactive)
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

* [gcx alert instances](gcx_alert_instances.md)	 - Manage alert instances.

