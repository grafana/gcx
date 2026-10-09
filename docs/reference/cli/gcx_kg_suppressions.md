## gcx kg suppressions

Manage alert suppressions in the Knowledge Graph.

### Options

```
  -h, --help   help for suppressions
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx kg](gcx_kg.md)	 - Manage Grafana Knowledge Graph rules, entities, and insights
* [gcx kg suppressions delete](gcx_kg_suppressions_delete.md)	 - Delete a suppression by name.
* [gcx kg suppressions list](gcx_kg_suppressions_list.md)	 - List all alert suppressions.
* [gcx kg suppressions upsert](gcx_kg_suppressions_upsert.md)	 - Upsert (create or update) one or more suppressions from a YAML file or stdin.

