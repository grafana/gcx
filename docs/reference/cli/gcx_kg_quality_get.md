## gcx kg quality get

Get the full quality report for a single entity.

```
gcx kg quality get <entity-name> [flags]
```

### Examples

```
  gcx kg quality get my-service --env <env>
  gcx kg quality get my-service --env <env> --namespace <namespace>
  gcx kg quality get my-service --env <env> --yaml
```

### Options

```
      --env string         Environment scope (required)
  -h, --help               help for get
      --jq string          jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string        Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --namespace string   Namespace scope
  -o, --output string      Output format. One of: agents, json, table, yaml (default "table")
      --site string        Site scope
      --type string        Entity type (default "Service")
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

* [gcx kg quality](gcx_kg_quality.md)	 - Inspect Knowledge Graph entity quality reports.

