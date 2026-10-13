## gcx assistant watchers

[experimental] Inspect Assistant Watchers.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

Read recurring telemetry-monitoring definitions and their current runtime observations. Use get for configuration and status for calibration, checks and assessments. These commands do not start monitoring or change Watchers.

### Examples

```
  gcx assistant watchers list
  gcx assistant watchers get checkout-health -o yaml
  gcx assistant watchers status checkout-health -o json
```

### Options

```
  -h, --help   help for watchers
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

* [gcx assistant](gcx_assistant.md)	 - Interact with Grafana Assistant
* [gcx assistant watchers get](gcx_assistant_watchers_get.md)	 - [experimental] Get an Assistant Watcher definition.
* [gcx assistant watchers list](gcx_assistant_watchers_list.md)	 - [experimental] List Assistant Watcher definitions.
* [gcx assistant watchers status](gcx_assistant_watchers_status.md)	 - [experimental] Inspect an Assistant Watcher's runtime status.

