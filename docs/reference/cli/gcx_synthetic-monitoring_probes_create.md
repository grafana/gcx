## gcx synthetic-monitoring probes create

Create a Synthetic Monitoring probe.

```
gcx synthetic-monitoring probes create [flags]
```

### Examples

```
  # Create a probe with a name and region.
  gcx synthetic-monitoring probes create --name my-probe --region eu

  # Create a probe with labels and coordinates.
  gcx synthetic-monitoring probes create --name my-probe --region us --labels env=prod,team=sre --latitude 37.7749 --longitude -122.4194
```

### Options

```
  -h, --help              help for create
      --jq string         jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string       Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --labels strings    Labels in key=value format
      --latitude float    Probe latitude
      --longitude float   Probe longitude
      --name string       Probe name (required)
  -o, --output string     Output format. One of: agents, json, text, yaml (default "text")
      --region string     Probe region
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

* [gcx synthetic-monitoring probes](gcx_synthetic-monitoring_probes.md)	 - Manage Synthetic Monitoring probes.

