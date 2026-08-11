## gcx kg notifications

Manage alert notification configs in the Knowledge Graph.

### Synopsis

Manage alert notification configs (AlertConfig) in the Knowledge Graph.

These govern how matched alerts notify — the labels an alert must match, extra
alert labels and annotations to attach, the "for" duration, and the silenced
flag. Distinct from "gcx kg suppressions", which manages disabled-alert configs.

### Options

```
  -h, --help   help for notifications
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
* [gcx kg notifications get](gcx_kg_notifications_get.md)	 - Get an alert notification config by name.
* [gcx kg notifications list](gcx_kg_notifications_list.md)	 - List alert notification configs, optionally filtered by category.

