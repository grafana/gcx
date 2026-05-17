## gcx datasources sql schema

Show the abstraction-schema a datasource exposes (tables, columns, hints, capabilities)

### Synopsis

Show the schema a datasource plugin exposes via the abstractionSchema
protocol. The default view lists table names plus datasource-level
capabilities. Use --table to drill into a single table's columns,
hints, and parameters.

Only datasources whose plugin implements the abstractionSchema endpoints
respond; others return 404 or 501.

```
gcx datasources sql schema DATASOURCE_UID [flags]
```

### Examples

```

  # List tables (with --match to filter when there are many)
  gcx datasources sql schema bfh6nkyxwj7cwf --match up

  # Drill into a single table's columns and hints
  gcx datasources sql schema bfh6nkyxwj7cwf --table up

  # Full schema as JSON
  gcx datasources sql schema bfh6nkyxwj7cwf -o json
```

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for schema
      --json string      Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int        Max number of tables to display in the list view (0 = unlimited) (default 200)
      --match string     Case-insensitive substring filter applied to table names
  -o, --output string    Output format. One of: agents, json, table, yaml (default "table")
      --table string     Drill into a specific table (show columns, hints, parameters)
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources sql](gcx_datasources_sql.md)	 - Cross-datasource SQL via the dsabstraction.grafana.app API

