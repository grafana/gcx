## gcx experiments update

[experimental] Update an Odin experiment.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

Update common fields with --title, --description, or --status. The command
fetches the current experiment, preserves its other fields, and sends the
current resourceVersion. For larger changes, pass the complete resource with
-f after running experiments get. Odin rejects a stale resourceVersion instead
of overwriting a concurrent edit.

```
gcx experiments update <name> [flags]
```

### Examples

```
  gcx experiments update checkout-conversion --title "New title"
  gcx experiments update checkout-conversion --description "Measure checkout conversion" --status draft
  gcx experiments get checkout-conversion -o yaml > experiment.yaml
  gcx experiments update checkout-conversion -f experiment.yaml
  gcx experiments update checkout-conversion -f - < experiment.json
```

### Options

```
      --description string   New experiment description
  -f, --filename string      Updated complete Experiment YAML or JSON manifest (use - for stdin)
  -h, --help                 help for update
      --jq string            jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string        Output format. One of: agents, json, yaml (default "yaml")
      --status string        New experiment lifecycle status
      --title string         New experiment title
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

