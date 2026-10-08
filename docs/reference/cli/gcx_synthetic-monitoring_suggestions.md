## gcx synthetic-monitoring suggestions

[experimental] Discover Synthetic Monitoring check suggestions.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

Check suggestions come from the Synthetic Monitoring Reliability Inbox, an
experimental service that analyses a stack's telemetry.

### Options

```
  -h, --help   help for suggestions
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx synthetic-monitoring](gcx_synthetic-monitoring.md)	 - Manage Grafana Synthetic Monitoring checks and probes
* [gcx synthetic-monitoring suggestions list](gcx_synthetic-monitoring_suggestions_list.md)	 - [experimental] List suggested checks generated from this stack's telemetry.

