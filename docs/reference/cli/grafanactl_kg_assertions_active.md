## grafanactl kg assertions active

Show entities with active assertions.

```
grafanactl kg assertions active [flags]
```

### Options

```
      --env string         Environment scope
  -h, --help               help for active
      --json string        Comma-separated list of fields to include in JSON output, or '?' to discover available fields
      --namespace string   Namespace scope
  -o, --output string      Output format. One of: json, yaml (default "json")
      --page int           Page number (0-based)
      --severity string    Filter by severity (e.g. CRITICAL, WARNING)
      --since string       Duration ago (e.g. 1h, 30m, 7d) — default 1h
      --site string        Site scope
      --type string        Filter by entity type
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GRAFANACTL_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [grafanactl kg assertions](grafanactl_kg_assertions.md)	 - Query Knowledge Graph assertions.

