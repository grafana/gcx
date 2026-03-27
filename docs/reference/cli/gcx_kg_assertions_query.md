## gcx kg assertions query

Query assertions for a time range.

```
gcx kg assertions query [Type--Name] [flags]
```

### Options

```
      --env string         Environment scope
  -f, --file string        Input file (YAML) — overrides all other flags
  -h, --help               help for query
      --name string        Entity name
      --namespace string   Namespace scope
      --since string       Duration ago (e.g. 1h, 30m, 7d) — default 1h
      --site string        Site scope
      --type string        Entity type
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

* [gcx kg assertions](gcx_kg_assertions.md)	 - Query Knowledge Graph assertions.

