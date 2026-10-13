## gcx cloud

Manage your Grafana Cloud resources

### Options

```
  -h, --help   help for cloud
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx cloud login](gcx_cloud_login.md)	 - Authenticate with the Grafana Cloud API (GCOM)
* [gcx cloud orgs](gcx_cloud_orgs.md)	 - Discover your Grafana Cloud organisations
* [gcx cloud stacks](gcx_cloud_stacks.md)	 - Manage Grafana Cloud stacks (list, create, update, delete)

