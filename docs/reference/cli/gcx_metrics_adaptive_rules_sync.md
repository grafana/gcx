## gcx metrics adaptive rules sync

Sync aggregation rules from a file or recommendations.

```
gcx metrics adaptive rules sync [flags]
```

### Options

```
      --dry-run                Print what would be synced without making changes
  -f, --file string            File containing rules to sync (JSON or YAML)
      --from-recommendations   Sync rules from current recommendations
  -h, --help                   help for sync
      --json string            Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string          Output format. One of: json, table, wide, yaml (default "table")
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

* [gcx metrics adaptive rules](gcx_metrics_adaptive_rules.md)	 - Manage aggregation rules.

