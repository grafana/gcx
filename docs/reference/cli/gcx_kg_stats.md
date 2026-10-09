## gcx kg stats

Show entity and active-insight counts, broken down by type, severity, and insight name.

```
gcx kg stats [flags]
```

### Options

```
      --env string         Environment scope
      --from string        Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help               help for stats
      --jq string          jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string        Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --namespace string   Namespace scope
  -o, --output string      Output format. One of: agents, json, yaml (default "json")
      --since string       Duration before --to (or now); mutually exclusive with --from/--to (e.g. 1h, 30m, 7d)
      --site string        Site scope
      --to string          End time (RFC3339, Unix timestamp, or relative like 'now')
      --type string        Limit to a specific entity type
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx kg](gcx_kg.md)	 - Manage Grafana Knowledge Graph rules, entities, and insights

