## gcx instrumentation apps

Manage instrumentation app configurations.

### Options

```
  -h, --help   help for apps
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

* [gcx instrumentation](gcx_instrumentation.md)	 - Manage Grafana Cloud instrumentation (clusters and apps)
* [gcx instrumentation apps create](gcx_instrumentation_apps_create.md)	 - Create an instrumentation app configuration from a file.
* [gcx instrumentation apps delete](gcx_instrumentation_apps_delete.md)	 - Delete an instrumentation app configuration.
* [gcx instrumentation apps get](gcx_instrumentation_apps_get.md)	 - Get an instrumentation app configuration by name.
* [gcx instrumentation apps list](gcx_instrumentation_apps_list.md)	 - List instrumentation app configurations.
* [gcx instrumentation apps update](gcx_instrumentation_apps_update.md)	 - Update an instrumentation app configuration from a file.

