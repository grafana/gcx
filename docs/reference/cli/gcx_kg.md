## gcx kg

Manage Grafana Knowledge Graph rules, entities, and insights

### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for kg
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx kg cypher](gcx_kg_cypher.md)	 - Run a read-only Cypher query against the Knowledge Graph.
* [gcx kg entities](gcx_kg_entities.md)	 - Manage Knowledge Graph entities.
* [gcx kg insights](gcx_kg_insights.md)	 - Search insights and fetch their backing metrics.
* [gcx kg meta](gcx_kg_meta.md)	 - Show Knowledge Graph metadata: entity types, valid env/namespace/site values, and telemetry query configs.
* [gcx kg model-rules](gcx_kg_model-rules.md)	 - Push model rules to the Knowledge Graph.
* [gcx kg open](gcx_kg_open.md)	 - Open the Knowledge Graph app in the browser.
* [gcx kg relabel-rules](gcx_kg_relabel-rules.md)	 - Push relabel rules to the Knowledge Graph.
* [gcx kg rules](gcx_kg_rules.md)	 - Manage Knowledge Graph prom rules.
* [gcx kg scopes](gcx_kg_scopes.md)	 - Manage Knowledge Graph entity scopes.
* [gcx kg status](gcx_kg_status.md)	 - Show Knowledge Graph stack status.
* [gcx kg summary](gcx_kg_summary.md)	 - Show a summary of entities and active insights, broken down by type, severity, and insight name.
* [gcx kg suppressions](gcx_kg_suppressions.md)	 - Manage alert suppressions in the Knowledge Graph.

