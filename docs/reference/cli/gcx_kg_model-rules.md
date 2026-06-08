## gcx kg model-rules

Manage model rules in the Knowledge Graph.

### Options

```
  -h, --help   help for model-rules
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx kg](gcx_kg.md)	 - Manage Grafana Knowledge Graph rules, entities, and insights
* [gcx kg model-rules create](gcx_kg_model-rules_create.md)	 - Upload model rules from a YAML file.
* [gcx kg model-rules delete](gcx_kg_model-rules_delete.md)	 - Delete a custom model rules configuration by name.
* [gcx kg model-rules get](gcx_kg_model-rules_get.md)	 - Get a custom model rules configuration by name.
* [gcx kg model-rules list](gcx_kg_model-rules_list.md)	 - List all custom model rules configurations.
* [gcx kg model-rules schema](gcx_kg_model-rules_schema.md)	 - Fetch the live JSON Schema for ModelRules from the backend.

