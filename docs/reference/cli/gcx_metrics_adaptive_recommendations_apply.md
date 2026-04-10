## gcx metrics adaptive recommendations apply

Apply specific or all recommendations as rules.

```
gcx metrics adaptive recommendations apply [<metric>...|--all] [flags]
```

### Options

```
      --all              Apply all recommendations (bulk)
      --dry-run          Preview without applying
  -h, --help             help for apply
      --segment string   Segment ID
      --yes              Skip confirmation prompt
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

* [gcx metrics adaptive recommendations](gcx_metrics_adaptive_recommendations.md)	 - Manage metric recommendations.

