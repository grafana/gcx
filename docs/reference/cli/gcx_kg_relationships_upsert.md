## gcx kg relationships upsert

Create or update a custom relationship (upsert) [experimental].

### Synopsis

Create or update an API-origin edge between two existing entities.
Both endpoints must already exist.

Experimental: this command uses the Knowledge Graph write API, which is gated
server-side and may change.

With -f, the input may be a single object or a YAML/JSON array. Array entries
are processed in order as independent upserts: the operation is not atomic,
and entries already written stay written if a later entry fails.

```
gcx kg relationships upsert [flags]
```

### Examples

```
  gcx kg relationships upsert --type CALLS --domain myapp \
    --from myapp/Service/checkout --to myapp/Service/cart --to-scope env=prod --ttl 1h
  gcx kg relationships upsert -f rel.yaml
```

### Options

```
      --domain string               Writable domain slug for the edge — a specific application domain such as 'irm' (required)
  -f, --file string                 Input file (YAML/JSON), or '-' for stdin; mutually exclusive with flags
      --from string                 Source entity ref as domain/Type/name (required)
      --from-scope stringToString   Scope for --from as key=value (repeatable or comma-separated) (default [])
  -h, --help                        help for upsert
      --jq string                   jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string                 Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string               Output format. One of: agents, json, table, yaml (default "json")
      --property stringToString     Property as key=value (repeatable or comma-separated) (default [])
      --to string                   Target entity ref as domain/Type/name (required)
      --to-scope stringToString     Scope for --to as key=value (repeatable or comma-separated) (default [])
      --ttl string                  Time-to-live duration (e.g. 1h, 7d); omitted = never expire
      --type string                 Relationship type (identifier; required)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx kg relationships](gcx_kg_relationships.md)	 - Manage custom Knowledge Graph relationships [experimental].

