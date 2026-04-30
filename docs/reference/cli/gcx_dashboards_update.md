## gcx dashboards update

Update a dashboard from a manifest

### Synopsis

Update a Grafana dashboard from a JSON or YAML manifest.

The manifest must include metadata.resourceVersion captured by a recent
'gcx dashboards get'. The server uses it for optimistic concurrency: if
the dashboard has been modified by another writer since the manifest was
fetched, the update fails with a conflict error and the hint to re-fetch.

Recommended workflow:

  gcx dashboards get <name> -o yaml > dashboard.yaml
  # edit dashboard.yaml
  gcx dashboards update <name> -f dashboard.yaml

```
gcx dashboards update <name> -f <file> [flags]
```

### Options

```
      --api-version string   API version to use (e.g. dashboard.grafana.app/v1); defaults to server preferred version
  -f, --filename string      Path to JSON/YAML manifest file ('-' reads from stdin)
  -h, --help                 help for update
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

