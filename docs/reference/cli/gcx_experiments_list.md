## gcx experiments list

[experimental] List Odin experiments.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

List experiments through the Odin app plugin on the selected Grafana instance.
The plugin returns at most 500 experiments in one page; if 500 are returned,
the list may be incomplete.

```
gcx experiments list [flags]
```

### Examples

```
  gcx experiments list
  gcx experiments list -o json
  gcx experiments list --jq '[.[] | {name: .metadata.name, title: .spec.title, status: .spec.status}]'
```

### Options

```
  -h, --help            help for list
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, table, wide, yaml (default "table")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx experiments](gcx_experiments.md)	 - [experimental] Work with Odin experiments.
