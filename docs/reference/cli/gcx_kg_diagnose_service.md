## gcx kg diagnose service

Diagnose a specific service in the Knowledge Graph.

### Synopsis

Deep diagnosis for a specific service: entity lookup, relationship
analysis, per-service recording rule checks, and interpreted diagnosis
with suggested next steps.

```
gcx kg diagnose service NAME [flags]
```

### Examples

```
  gcx kg diagnose service api-gateway
  gcx kg diagnose service payment-service --env production
  gcx kg diagnose service checkout --env production --namespace default -o json
```

### Options

```
  -d, --datasource string   Prometheus datasource UID (auto-discovered if omitted)
      --env string          Environment scope
  -h, --help                help for service
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --namespace string    Namespace scope
  -o, --output string       Output format. One of: agents, json, text, yaml (default "text")
      --site string         Site scope
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx kg diagnose](gcx_kg_diagnose.md)	 - Run diagnostic checks on the Knowledge Graph pipeline.

