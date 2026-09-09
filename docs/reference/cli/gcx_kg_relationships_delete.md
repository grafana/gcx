## gcx kg relationships delete

[experimental] Delete a custom relationship.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Delete an API-origin edge of the given type between the from/to entities.
The endpoint refs (incl. scope) must match the values used at upsert.

This command uses the Knowledge Graph write API, which is gated server-side.

```
gcx kg relationships delete [flags]
```

### Examples

```
  gcx kg relationships delete --type CALLS \
    --from myapp/Service/checkout --to myapp/Service/cart --force
```

### Options

```
      --force                       Skip confirmation prompt
      --from string                 Source entity ref as domain/Type/name (required)
      --from-scope stringToString   Scope for --from as key=value (repeatable or comma-separated) (default [])
  -h, --help                        help for delete
      --jq string                   jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string                 Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string               Output format. One of: agents, json, text, yaml (default "text")
      --to string                   Target entity ref as domain/Type/name (required)
      --to-scope stringToString     Scope for --to as key=value (repeatable or comma-separated) (default [])
      --type string                 Relationship type (identifier; required)
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

* [gcx kg relationships](gcx_kg_relationships.md)	 - [experimental] Manage custom Knowledge Graph relationships.

