## gcx datasources schemas

Inspect datasource plugin schemas

### Synopsis

Inspect the configuration schema of a datasource plugin type, to author manifests correctly.

### Options

```
  -h, --help   help for schemas
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources](gcx_datasources.md)	 - Manage and query Grafana datasources
* [gcx datasources schemas get](gcx_datasources_schemas_get.md)	 - Get the configuration schema for a datasource plugin type
* [gcx datasources schemas list](gcx_datasources_schemas_list.md)	 - List datasource plugin types installed on the Grafana instance

