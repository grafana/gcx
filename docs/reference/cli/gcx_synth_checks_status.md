## gcx synth checks status

Show pass/fail status of Synthetic Monitoring checks.

### Synopsis

Show pass/fail status by combining the SM API with Prometheus probe_success metrics.

Displays current success rate, number of probes reporting, and health status
for each check. Requires a Prometheus datasource containing SM metrics.

```
gcx synth checks status [ID] [flags]
```

### Examples

```
  # Show status of all checks.
  gcx synth checks status

  # Show status of a specific check by ID.
  gcx synth checks status 42

  # Specify the Prometheus datasource to query.
  gcx synth checks status --datasource-uid my-prometheus

  # Output as JSON for scripting.
  gcx synth checks status -o json
```

### Options

```
      --datasource-uid string   UID of the Prometheus datasource to query
  -h, --help                    help for status
      --json string             Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string           Output format. One of: graph, json, table, wide, yaml (default "table")
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx synth checks](gcx_synth_checks.md)	 - Manage Synthetic Monitoring checks.

