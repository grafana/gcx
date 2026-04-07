## gcx kg search entities

Search for entities by type.

```
gcx kg search entities [flags]
```

### Options

```
      --env string         Environment scope
  -h, --help               help for entities
      --json string        Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --namespace string   Namespace scope
  -o, --output string      Output format. One of: json, yaml (default "json")
      --page int           Page number (0-based)
      --since string       Duration ago (e.g. 1h, 30m, 7d) — default 1h
      --site string        Site scope
      --type string        Entity type (omit to search all)
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx kg search](gcx_kg_search.md)	 - Search Knowledge Graph entities or insights.

