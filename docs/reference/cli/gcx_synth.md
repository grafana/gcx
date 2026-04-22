## gcx synth

Manage Grafana Synthetic Monitoring checks and probes

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for synth
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx synth checks](gcx_synth_checks.md)	 - Manage Synthetic Monitoring checks.
* [gcx synth probes](gcx_synth_probes.md)	 - Manage Synthetic Monitoring probes.
* [gcx synth setup](gcx_synth_setup.md)	 - Set up synth (not yet implemented).

