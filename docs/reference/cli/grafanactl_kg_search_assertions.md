## grafanactl kg search assertions

Search for assertions matching a query.

```
grafanactl kg search assertions [flags]
```

### Options

```
      --env string         Environment scope
  -f, --file string        Input file (YAML)
  -h, --help               help for assertions
      --name string        Entity name filter
      --namespace string   Namespace scope
      --since string       Duration ago (e.g. 1h, 30m, 7d) — default 1h
      --site string        Site scope
      --type string        Entity type filter
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

* [grafanactl kg search](grafanactl_kg_search.md)	 - Search Knowledge Graph entities or assertions.

