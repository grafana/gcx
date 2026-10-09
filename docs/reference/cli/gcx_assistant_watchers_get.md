## gcx assistant watchers get

[experimental] Get an Assistant Watcher definition.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

Read a configuration-only manifest by resource name or server ID. Names are derived from titles; an ambiguous name reports candidate IDs. JSON and YAML match gcx resources get watchers/WATCHER. Secret values are never exported; configured secrets use preserve markers. Use status for runtime observations.

```
gcx assistant watchers get WATCHER [flags]
```

### Examples

```
  gcx assistant watchers get checkout-health
  gcx assistant watchers get checkout-health -o yaml
  gcx assistant watchers get example-id -o json
```

### Options

```
  -h, --help            help for get
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

