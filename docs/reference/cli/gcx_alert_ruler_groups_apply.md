## gcx alert ruler groups apply

Create or update ruler rule groups from a file.

### Synopsis

Create or update rule groups in a ruler namespace. The input may be a
standard Prometheus rules file (with a top-level "groups:" list) or a single
bare rule group. Applying a group replaces the group with the same name.

```
gcx alert ruler groups apply [flags]
```

### Options

```
      --datasource string   Datasource UID of the Mimir/Loki ruler (required)
      --dry-run             Parse and validate only; send nothing to the ruler
  -f, --filename string     File containing rule groups (Prometheus rules file or a single group; YAML/JSON, use - for stdin)
  -h, --help                help for apply
      --namespace string    Ruler namespace to apply the groups to (required)
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

* [gcx alert ruler groups](gcx_alert_ruler_groups.md)	 - Manage ruler rule groups.

