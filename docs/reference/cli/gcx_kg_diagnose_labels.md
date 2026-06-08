## gcx kg diagnose labels

Validate the deployment_environment → asserts_env label pipeline.

### Synopsis

Check that deployment_environment values in raw metrics are correctly
mapped to asserts_env in recording rule outputs. Identifies unmapped
environments (services that won't appear in Entity Graph) and orphaned
asserts_env values with no deployment_environment source.

```
gcx kg diagnose labels [flags]
```

### Examples

```
  gcx kg diagnose labels
  gcx kg diagnose labels --datasource grafanacloud-prom
  gcx kg diagnose labels -o json
```

### Options

```
  -d, --datasource string   Prometheus datasource UID (auto-discovered if omitted)
  -h, --help                help for labels
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string       Output format. One of: agents, json, table, yaml (default "table")
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

* [gcx kg diagnose](gcx_kg_diagnose.md)	 - Run diagnostic checks on the Knowledge Graph pipeline.

