## grafanactl kg entities show

Show entities. Without a name, lists all; with a name, shows one.

```
grafanactl kg entities show [name] [flags]
```

### Options

```
      --assertions-only    Only return entities with active assertions (list mode)
      --env string         Environment scope
  -h, --help               help for show
      --json string        Comma-separated list of fields to include in JSON output, or '?' to discover available fields
      --namespace string   Namespace scope
  -o, --output string      Output format. One of: json, table, yaml (default "table")
      --page int           Page number, 0-based (list mode)
      --since string       Duration ago (e.g. 1h, 30m, 7d) — default 1h
      --site string        Site scope
      --type string        Entity type (required for single entity, optional for list)
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

* [grafanactl kg entities](grafanactl_kg_entities.md)	 - Manage Knowledge Graph entities.

