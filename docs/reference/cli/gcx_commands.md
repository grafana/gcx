## gcx commands

List all commands with rich metadata for agent consumption

### Synopsis

Output a hierarchical catalog of all CLI commands with metadata including
flags, arguments, token cost estimates, and agent hints. Also includes a
resource_types section listing all known Grafana resource types.

Use --validate with a configured Grafana context to compare the catalog
against live resource discovery and report uncovered or stale types.

```
gcx commands [flags]
```

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --flat             Flatten the command tree into a single list
  -h, --help             help for commands
      --include-hidden   Include hidden commands in the output
      --json string      Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string    Output format. One of: json, text, yaml (default "json")
      --validate         Validate catalog against a live Grafana instance (requires configured context)
```

### Options inherited from parent commands

```
      --agent           Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --no-color        Disable color output
      --no-truncate     Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count   Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations

