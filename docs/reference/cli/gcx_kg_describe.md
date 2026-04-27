## gcx kg describe

Load Knowledge Graph schema, scope values, and telemetry drilldown configs.

### Synopsis

Load Knowledge Graph metadata needed to formulate correct KG and telemetry queries.

By default all sections are loaded. Use flags to request specific sections:
  --schema    Entity types, properties, and relationships
  --scopes    Available env/site/namespace values
  --logs      Log drilldown configs (entity property → Loki label mappings)
  --traces    Trace drilldown configs (entity property → Tempo label mappings)
  --profiles  Profile drilldown configs (entity property → Pyroscope label mappings)

```
gcx kg describe [flags]
```

### Examples

```
  # Load everything (default)
  gcx kg describe

  # Schema and scopes only — useful before a gcx kg traverse call
  gcx kg describe --schema --scopes

  # Log configs only — use before building a Loki query from entity properties
  gcx kg describe --logs

  # All telemetry configs as JSON
  gcx kg describe --logs --traces --profiles -o json
```

### Options

```
      --env string         Environment scope
      --from string        Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help               help for metadata
      --json string        Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --logs               Load log drilldown configs (entity property → Loki label mappings)
      --namespace string   Namespace scope
  -o, --output string      Output format. One of: json, text, yaml (default "text")
      --profiles           Load profile drilldown configs (entity property → Pyroscope label mappings)
      --schema             Load entity types, properties, and relationships
      --scopes             Load available env/site/namespace values
      --since string       Duration before --to (or now); mutually exclusive with --from (e.g. 1h, 30m, 7d)
      --site string        Site scope
      --to string          End time (RFC3339, Unix timestamp, or relative like 'now')
      --traces             Load trace drilldown configs (entity property → Tempo label mappings)
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx kg](gcx_kg.md)	 - Manage Grafana Knowledge Graph rules, entities, and insights

