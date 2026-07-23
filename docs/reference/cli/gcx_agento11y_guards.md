## gcx agento11y guards

Manage synchronous policy guards (hook rules) that evaluate generations on the request path.

### Synopsis

Guards (hook rules) are synchronous policies evaluated before or after each generation.
They can deny, warn, or transform a generation based on evaluators, regex patterns, or blocked tool names.

Unlike eval rules (gcx agento11y rules), guards run inline on the request path and short-circuit by default.

### Options

```
  -h, --help   help for guards
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

* [gcx agento11y](gcx_agento11y.md)	 - Manage Grafana Agent Observability resources
* [gcx agento11y guards create](gcx_agento11y_guards_create.md)	 - Create a hook rule (guard) from a file.
* [gcx agento11y guards delete](gcx_agento11y_guards_delete.md)	 - Delete hook rules (guards).
* [gcx agento11y guards get](gcx_agento11y_guards_get.md)	 - Get a single hook rule (guard).
* [gcx agento11y guards list](gcx_agento11y_guards_list.md)	 - List hook rules (guards).
* [gcx agento11y guards update](gcx_agento11y_guards_update.md)	 - Update a hook rule (guard) from a file. Full replace; omitted fields reset to defaults.

