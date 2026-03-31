## gcx adaptive logs patterns apply

Apply drop rate recommendations to adaptive log patterns.

```
gcx adaptive logs patterns apply [SUBSTRING] [flags]
```

### Options

```
      --all            Apply to all patterns
      --dry-run        Preview changes without making them
  -h, --help           help for apply
      --rate float32   Drop rate to apply (0.0–1.0); defaults to recommended_drop_rate if not set
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

* [gcx adaptive logs patterns](gcx_adaptive_logs_patterns.md)	 - Manage adaptive log patterns.

