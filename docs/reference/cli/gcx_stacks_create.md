## gcx stacks create

Create a new Grafana Cloud stack.

### Synopsis

Create a new Grafana Cloud stack.

This command creates a new Grafana Cloud stack, which provisions infrastructure
and may incur costs. Always confirm the stack name, slug, and region with the
user before executing. Prefer --dry-run first.

```
gcx stacks create [flags]
```

### Options

```
      --delete-protection    Enable delete protection
      --description string   Short description
      --dry-run              Preview the request without executing it
  -h, --help                 help for create
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --labels strings       Labels in key=value format (may be repeated)
      --name string          Stack name (required)
  -o, --output string        Output format. One of: json, table, yaml (default "table")
      --region string        Region slug (e.g. us, eu). Use 'gcx stacks regions' to list.
      --slug string          Stack slug / subdomain (required)
      --url string           Custom domain URL
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

* [gcx stacks](gcx_stacks.md)	 - Manage Grafana Cloud stacks (list, create, update, delete)

