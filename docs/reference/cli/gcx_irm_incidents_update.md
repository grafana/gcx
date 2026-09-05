## gcx irm incidents update

Update the severity or the title of an incident.

### Synopsis

Update the severity or the title of an incident.

The severity is the display label, not the identifier. Run
`gcx irm incidents severities list` for the labels of your organization.

gcx reads the incident first, so a value that already matches causes no write.
The command prints one line that names the fields it changed. Use -o json or
-o yaml for a structured update result.

```
gcx irm incidents update <id> [flags]
```

### Examples

```
  # Raise the severity of an incident:
  gcx irm incidents update 4 --severity Critical

  # Correct the title:
  gcx irm incidents update 4 --title "Checkout latency above the objective"
```

### Options

```
  -h, --help              help for update
      --jq string         jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string       Comma-separated list of dotted field paths to include in JSON output (e.g. spec.name), or 'list' (or '?') to discover the available paths
  -o, --output string     Output format. One of: agents, json, text, yaml (default "text")
      --severity string   New severity label (run 'gcx irm incidents severities list' for the valid values)
      --title string      New title
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx irm incidents](gcx_irm_incidents.md)	 - Manage incidents.

