## gcx synth checks create

Create a Synthetic Monitoring check from a file.

```
gcx synth checks create [flags]
```

### Examples

```
  # Create a check from a YAML file.
  gcx synth checks create -f check.yaml

  # Create and show resulting status.
  gcx synth checks create -f check.yaml --show-status

  # Validate HTTP target before creating.
  gcx synth checks create -f check.yaml --validate-targets
```

### Options

```
  -f, --filename string    File containing the check manifest (YAML)
  -h, --help               help for create
      --show-status        Query and display check status after creation
      --validate-targets   Pre-flight HTTP HEAD request for HTTP check targets (warning only)
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx synth checks](gcx_synth_checks.md)	 - Manage Synthetic Monitoring checks.

