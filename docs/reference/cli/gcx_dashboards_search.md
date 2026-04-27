## gcx dashboards search

Search dashboards by title, tag, or folder

### Synopsis

Search dashboards using the Grafana full-text search API.

The search endpoint is pinned to the v0alpha1 API version and does not support
--api-version overrides. Use 'gcx dashboards list' to list dashboards with the
server-preferred API version.

An empty positional query is accepted when at least one --folder or --tag
filter is supplied.

```
gcx dashboards search [query] [flags]
```

### Examples

```
  # Search by title.
  gcx dashboards search "my dashboard"

  # Search within a folder.
  gcx dashboards search --folder my-folder-name

  # Search by tag with multiple folders.
  gcx dashboards search --tag prod --folder folder-a --folder folder-b

  # Output as YAML.
  gcx dashboards search "metrics" -o yaml
```

### Options

```
      --deleted              Include recently deleted dashboards
      --folder stringArray   Filter by folder name (repeatable)
  -h, --help                 help for search
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int            Maximum number of results (0 for no limit) (default 50)
  -o, --output string        Output format. One of: json, table, wide, yaml (default "table")
      --sort string          Sort key (e.g. name_sort)
      --tag stringArray      Filter by tag (repeatable)
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

* [gcx dashboards](gcx_dashboards.md)	 - Manage Grafana dashboards

