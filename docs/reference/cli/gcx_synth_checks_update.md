## gcx synth checks update

Update a Synthetic Monitoring check from a file.

```
gcx synth checks update <name> [flags]
```

### Examples

```
  # Update a check using its resource name.
  gcx synth checks update web-check-1234 -f check.yaml

  # Update and show previous status.
  gcx synth checks update web-check-1234 -f check.yaml --show-status
```

### Options

```
  -f, --filename string    File containing the check manifest (YAML)
  -h, --help               help for update
      --show-status        Query and display the previous check status after update
      --validate-targets   Pre-flight HTTP HEAD request for HTTP check targets (warning only)
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx synth checks](gcx_synth_checks.md)	 - Manage Synthetic Monitoring checks.

