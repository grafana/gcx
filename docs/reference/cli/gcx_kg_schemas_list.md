## gcx kg schemas list

[experimental] List effective declared Knowledge Graph schemas.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

List installed schema domains and versions for the configured stack namespace.
Use this command to discover declared schemas, even when their entity types
do not appear in the graph. Use 'gcx kg meta schema' for observed graph metadata.

By default, the server returns only the highest-priority version per domain
and omits bundle definitions. Version priority follows Kubernetes ordering:
stable versions outrank beta versions, which outrank alpha versions.

Use --expand with JSON or YAML output to include imports, entity types,
relationship types, and relationship type bindings. The table shows domain
metadata only. Use --latest-only=false to include all installed versions.

The schema discovery endpoint requires the Knowledge Graph write API to be
enabled on the stack. This command only reads schemas; it does not modify them.

```
gcx kg schemas list [flags]
```

### Examples

```
  gcx kg schemas list
  gcx kg schemas list --expand -o json
  gcx kg schemas list --expand --latest-only=false -o yaml
```

### Options

```
      --expand          Include complete schema bundles; use JSON or YAML to see the definitions
  -h, --help            help for list
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --latest-only     Return only the highest-priority version per domain; set false to include all installed versions (default true)
  -o, --output string   Output format. One of: agents, json, table, yaml (default "table")
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

* [gcx kg schemas](gcx_kg_schemas.md)	 - Discover installed Knowledge Graph schemas.

