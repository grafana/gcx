## gcx agento11y model-rates delete

Delete one configured rate.

### Synopsis

Delete one configured rate.

A model can carry several rates, one per change, so a delete names which by
its effective-from. Take that value from 'list'.

Deleting does not restore the catalog price for generations this rate already
priced: their cost was computed when they arrived and is not recalculated.
What changes is the price applied from now on.

```
gcx agento11y model-rates delete [flags]
```

### Examples

```
  # Take effective-from from list, then delete that rate.
  gcx agento11y model-rates list
  gcx agento11y model-rates delete --provider openai --model gpt-5.5 \
      --effective-from 2026-09-23T09:14:22.481739Z
```

### Options

```
      --effective-from string   effective-from of the rate to delete, as 'list' reports it (required)
      --force                   Skip confirmation prompt
  -h, --help                    help for delete
      --jq string               jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string             Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --model string            Model of the rate to delete (required)
  -o, --output string           Output format. One of: agents, json, text, yaml (default "text")
      --provider string         Provider of the rate to delete (required)
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

* [gcx agento11y model-rates](gcx_agento11y_model-rates.md)	 - Configure your own negotiated model prices.

