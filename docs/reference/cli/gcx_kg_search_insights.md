## gcx kg search insights

Search for insights matching a query.

```
gcx kg search insights [flags]
```

### Examples

```
  # Search all Service insights
  gcx kg search insights --type Service

  # Search insights for a named entity
  gcx kg search insights --type Service --name api-server --env prod

  # Supply a full request as a YAML file
  gcx kg search insights --file request.yaml

  # Example request.yaml:
  #
  #   filterCriteria:
  #     - entityType: Service
  #       havingAssertion: true
  #   timeCriteria:
  #     start: 1700000000
  #     end:   1700003600
```

### Options

```
      --env string         Environment scope
  -f, --file string        Input file (YAML)
      --from string        Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help               help for insights
      --name string        Entity name filter
      --namespace string   Namespace scope
      --since string       Duration before --to (or now); mutually exclusive with --from (e.g. 1h, 30m, 7d)
      --site string        Site scope
      --to string          End time (RFC3339, Unix timestamp, or relative like 'now')
      --type string        Entity type filter
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

* [gcx kg search](gcx_kg_search.md)	 - Search Knowledge Graph entities or insights.

