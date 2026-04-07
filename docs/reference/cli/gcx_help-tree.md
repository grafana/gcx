## gcx help-tree

Print a compact command tree for agent context injection

### Synopsis

Outputs a token-efficient text tree of the CLI hierarchy with inline args,
flags, and agent hints. Designed for injecting into agent context windows.

Use positional arguments to show only a subtree (e.g., "gcx help-tree resources get").
Use --depth to limit nesting depth.

```
gcx help-tree [COMMAND...] [flags]
```

### Options

```
      --depth int       Maximum nesting depth (1 = root + direct children, 0 = unlimited)
  -h, --help            help for help-tree
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: json, text, yaml (default "text")
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string   Name of the context to use (overrides current-context in config)
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations

