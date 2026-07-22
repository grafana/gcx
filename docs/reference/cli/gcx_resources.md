## gcx resources

Manipulate Grafana resources

### Synopsis

Manipulate Grafana resources.

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for resources
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx resources delete](gcx_resources_delete.md)	 - Delete resources from Grafana
* [gcx resources edit](gcx_resources_edit.md)	 - Edit resources from Grafana
* [gcx resources get](gcx_resources_get.md)	 - Get resources from Grafana
* [gcx resources list-examples](gcx_resources_list-examples.md)	 - List example manifests for resource types
* [gcx resources list-types](gcx_resources_list-types.md)	 - List available Grafana API resource types
* [gcx resources pull](gcx_resources_pull.md)	 - Pull resources from Grafana
* [gcx resources push](gcx_resources_push.md)	 - Push resources to Grafana
* [gcx resources validate](gcx_resources_validate.md)	 - Validate local resources against a Grafana instance

