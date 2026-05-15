## gcx vulnobs projects list

List projects with CVE counts.

```
gcx vulnobs projects list [flags]
```

### Options

```
      --first int       Maximum number of projects to return (default 30)
      --group string    Filter to one group (name or numeric id)
  -h, --help            help for list
      --include-k8s     Include k8s-scan versions
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, table, yaml (default "table")
      --show-archived   Include archived sources
      --sort string     Sort order: CRITICALS_DESC, HIGHS_DESC, SLO_ASC (default "CRITICALS_DESC")
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

