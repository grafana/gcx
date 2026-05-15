## gcx vulnobs projects list-issues

List CVE findings for a project version (sub-resource).

### Synopsis

List CVE findings for a project version.

Issues are sub-resources of a Source's Version: every query requires a
versionId. Pass it either as a positional argument or resolve it from
--repo (and optionally --tag, default "main").

Examples:

  gcx vulnobs projects list-issues 10355
  gcx vulnobs projects list-issues --repo grafana/faro-web-sdk
  gcx vulnobs projects list-issues --repo grafana/faro-web-sdk --tag v2.6.3 \
      --severity CRITICAL,HIGH


```
gcx vulnobs projects list-issues [<versionId>] [flags]
```

### Options

```
  -h, --help              help for list-issues
      --json string       Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string     Output format. One of: agents, json, table, yaml (default "table")
      --repo string       Resolve from repo (owner/name); alternative to positional versionId
      --severity string   Comma-separated severities to include (CRITICAL,HIGH,...)
      --tag string        Tag to resolve when --repo is used (default "main")
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

* [gcx vulnobs projects](gcx_vulnobs_projects.md)	 - Manage vulnerability-obs projects (sources) and their findings.

