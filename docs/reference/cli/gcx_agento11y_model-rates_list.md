## gcx agento11y model-rates list

List the rates you have configured.

### Synopsis

List the rates you have configured.

Superseded rates are listed too. A change records a new rate rather than
replacing the old one, so a model can appear more than once — the newest
effective-from is the one in force, and the others are what priced the
generations that arrived while they applied.

```
gcx agento11y model-rates list [flags]
```

### Examples

```
  # The rates you have configured, up to --limit (default 50), newest first per model.
  gcx agento11y model-rates list

  # With every rate column.
  gcx agento11y model-rates list -o wide
```

### Options

```
  -h, --help            help for list
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int       Maximum number of rates to return (0 for no limit) (default 50)
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

* [gcx agento11y model-rates](gcx_agento11y_model-rates.md)	 - Configure your own negotiated model prices.

