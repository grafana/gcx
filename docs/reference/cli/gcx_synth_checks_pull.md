## gcx synth checks pull

Pull Synthetic Monitoring checks to disk.

```
gcx synth checks pull [flags]
```

### Options

```
  -h, --help            help for pull
  -d, --output string   Directory to write check YAML files to (default ".")
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

