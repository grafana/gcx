## gcx assistant watchers status

[experimental] Inspect an Assistant Watcher's runtime status.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

Read lifecycle, the last assessment, available run timestamps, estimated usage, audit and current version, calibration progress and calibrated checks. Lifecycle and health are separate observations; missing observations remain absent or unknown. Unsupported checks remain visible. This command does not request calibration or execute a run. Continue failed or incomplete calibration in Grafana.

```
gcx assistant watchers status WATCHER [flags]
```

### Examples

```
  gcx assistant watchers status checkout-health
  gcx assistant watchers status checkout-health -o yaml
  gcx assistant watchers status example-id -o json
```

### Options

```
  -h, --help            help for status
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, table, text, wide, yaml (default "text")
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

* [gcx assistant watchers](gcx_assistant_watchers.md)	 - [experimental] Inspect Assistant Watchers.

