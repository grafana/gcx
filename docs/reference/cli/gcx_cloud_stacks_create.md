## gcx cloud stacks create

Create a new Grafana Cloud stack.

### Synopsis

Create a new Grafana Cloud stack.

This provisions new infrastructure and may incur costs. The stack name, slug,
and region cannot be changed after creation - double-check before running.
Use --dry-run to preview the request first.

```
gcx cloud stacks create [flags]
```

### Options

```
      --delete-protection    Enable delete protection
      --description string   Short description
      --dry-run              Preview the request without executing it
  -h, --help                 help for create
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --labels strings       Labels in key=value format (may be repeated)
      --name string          Stack name (required)
  -o, --output string        Output format. One of: agents, json, table, yaml (default "table")
      --region string        Region slug (e.g. us, eu). Use 'gcx cloud stacks list-regions' to list.
      --slug string          Stack slug / subdomain (required)
      --url string           Custom domain URL
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

* [gcx cloud stacks](gcx_cloud_stacks.md)	 - Manage Grafana Cloud stacks (list, create, update, delete)

