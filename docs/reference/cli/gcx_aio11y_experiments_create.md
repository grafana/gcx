## gcx aio11y experiments create

Create a new experiment from a JSON or YAML file.

```
gcx aio11y experiments create [flags]
```

### Examples

```
  # Create from a YAML file.
  gcx aio11y experiments create -f experiment.yaml

  # Create from stdin.
  cat experiment.json | gcx aio11y experiments create -f -
```

### Options

```
  -f, --filename string   File containing the experiment create payload (use - for stdin)
  -h, --help              help for create
      --jq string         jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string       Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string     Output format. One of: agents, json, yaml (default "json")
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx aio11y experiments](gcx_aio11y_experiments.md)	 - Manage eval experiment runs.

