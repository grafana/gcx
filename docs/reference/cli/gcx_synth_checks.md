## gcx synth checks

Manage Synthetic Monitoring checks.

### Options

```
  -h, --help   help for checks
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

* [gcx synth](gcx_synth.md)	 - Manage Grafana Synthetic Monitoring checks and probes
* [gcx synth checks create](gcx_synth_checks_create.md)	 - Create a Synthetic Monitoring check from a file.
* [gcx synth checks delete](gcx_synth_checks_delete.md)	 - Delete Synthetic Monitoring checks.
* [gcx synth checks get](gcx_synth_checks_get.md)	 - Get a single Synthetic Monitoring check.
* [gcx synth checks list](gcx_synth_checks_list.md)	 - List Synthetic Monitoring checks.
* [gcx synth checks status](gcx_synth_checks_status.md)	 - Show pass/fail status of Synthetic Monitoring checks.
* [gcx synth checks timeline](gcx_synth_checks_timeline.md)	 - Render probe_success over time as a terminal line chart.
* [gcx synth checks update](gcx_synth_checks_update.md)	 - Update a Synthetic Monitoring check from a file.

