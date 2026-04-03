## gcx logs adaptive drop-rules

Manage adaptive log drop rules.

### Synopsis

Manage adaptive log drop rules.

Use `list` or `get` to read rules. `list` applies the __global__ segment filter (same scope as create/update/delete). `gcx resources get droprules` lists all tenant rules without that filter when the resources command is available.

Create and update load a rule from a file (`--filename` / `-f`), similar to Adaptive Traces policies. The file's top-level "version" is the policy schema version (only 1); omit or set it to 1 — not the revision field in API JSON.

### Options

```
  -h, --help   help for drop-rules
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

* [gcx logs adaptive](gcx_logs_adaptive.md)	 - Manage Adaptive Logs resources
* [gcx logs adaptive drop-rules create](gcx_logs_adaptive_drop-rules_create.md)	 - Create an adaptive log drop rule from a file.
* [gcx logs adaptive drop-rules delete](gcx_logs_adaptive_drop-rules_delete.md)	 - Delete an adaptive log drop rule.
* [gcx logs adaptive drop-rules get](gcx_logs_adaptive_drop-rules_get.md)	 - Fetch one adaptive log drop rule by ID.
* [gcx logs adaptive drop-rules list](gcx_logs_adaptive_drop-rules_list.md)	 - List adaptive log drop rules.
* [gcx logs adaptive drop-rules update](gcx_logs_adaptive_drop-rules_update.md)	 - Update an adaptive log drop rule by ID.

