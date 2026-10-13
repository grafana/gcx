## gcx assistant watchers list

[experimental] List Assistant Watcher definitions.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

Fetch every page of Watcher definitions visible to your configured identity. By default, lists non-archived Watchers; --archived selects archived Watchers only. Coverage is permission-scoped and is not an atomic snapshot. Partial reads retain readable items, mark coverage incomplete, and report failed or skipped identities; failed item reads return a nonzero exit status. Use get WATCHER for one definition and status WATCHER for runtime observations.

```
gcx assistant watchers list [flags]
```

### Examples

```
  gcx assistant watchers list
  gcx assistant watchers list --archived
  gcx assistant watchers list -o json
  gcx assistant watchers list -o wide
```

### Options

```
      --archived        List archived Watchers only; default lists non-archived Watchers visible to the caller
  -h, --help            help for list
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

