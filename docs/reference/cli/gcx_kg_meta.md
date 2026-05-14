## gcx kg meta

Show Knowledge Graph metadata: entity types, valid env/namespace/site values, and telemetry query configs.

### Options

```
  -h, --help   help for meta
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx kg](gcx_kg.md)	 - Manage Grafana Knowledge Graph rules, entities, and insights
* [gcx kg meta all](gcx_kg_meta_all.md)	 - Load all sections: schema, scopes, logs, traces, and profiles.
* [gcx kg meta logs](gcx_kg_meta_logs.md)	 - Show Loki label mappings for log drilldown.
* [gcx kg meta profiles](gcx_kg_meta_profiles.md)	 - Show Pyroscope label mappings for profile drilldown.
* [gcx kg meta schema](gcx_kg_meta_schema.md)	 - Show entity types, properties, and relationships.
* [gcx kg meta scopes](gcx_kg_meta_scopes.md)	 - Show all valid env/namespace/site filter values.
* [gcx kg meta traces](gcx_kg_meta_traces.md)	 - Show Tempo label mappings for trace drilldown.

