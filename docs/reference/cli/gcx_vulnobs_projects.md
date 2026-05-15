## gcx vulnobs projects

Manage vulnerability-obs projects (sources) and their findings.

### Options

```
  -h, --help   help for projects
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

* [gcx vulnobs](gcx_vulnobs.md)	 - Inspect Grafana Vulnerability Observability data (read-only).
* [gcx vulnobs projects list](gcx_vulnobs_projects_list.md)	 - List projects with CVE counts.
* [gcx vulnobs projects list-issues](gcx_vulnobs_projects_list-issues.md)	 - List CVE findings for a project version (sub-resource).

