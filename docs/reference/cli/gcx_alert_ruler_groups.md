## gcx alert ruler groups

Manage datasource-managed (Mimir/Loki ruler) rule groups.

### Options

```
  -h, --help   help for groups
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

* [gcx alert ruler](gcx_alert_ruler.md)	 - Manage datasource-managed (Mimir/Loki ruler) rules.
* [gcx alert ruler groups apply](gcx_alert_ruler_groups_apply.md)	 - Create or update ruler rule groups from a file.
* [gcx alert ruler groups delete](gcx_alert_ruler_groups_delete.md)	 - Delete a ruler rule group.
* [gcx alert ruler groups get](gcx_alert_ruler_groups_get.md)	 - Get a ruler rule group (YAML by default, round-trips into apply).
* [gcx alert ruler groups list](gcx_alert_ruler_groups_list.md)	 - List ruler rule groups.

