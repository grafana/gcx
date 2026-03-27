## gcx oncall alert-groups silence

Silence an alert group for a specified duration.

```
gcx oncall alert-groups silence <id> [flags]
```

### Options

```
      --duration int    Duration to silence in seconds (default 3600)
  -h, --help            help for silence
      --json string     Comma-separated list of fields to include in JSON output, or '?' to discover available fields
  -o, --output string   Output format. One of: json, yaml (default "text")
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

* [gcx oncall alert-groups](gcx_oncall_alert-groups.md)	 - Manage alert groups.

