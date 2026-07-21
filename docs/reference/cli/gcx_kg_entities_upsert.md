## gcx kg entities upsert

Create or update a custom entity (upsert) [experimental].

### Synopsis

Create or update an API-origin entity in a writable domain.

Experimental: this command uses the Knowledge Graph write API, which is gated
server-side and may change. If the write API is not enabled on your stack, the
server returns an error explaining how to request access.

Identity is (type, name, scope) + domain; re-running with the same identity
updates the entity. Scope is optional but identity-significant.

```
gcx kg entities upsert [flags]
```

### Examples

```
  gcx kg entities upsert --domain myapp --type Service --name checkout --scope env=prod --ttl 1h
  gcx kg entities upsert -f entity.yaml
```

### Options

```
      --domain string             Writable domain slug — a specific application domain such as 'irm' (required; 'kg' is reserved)
  -f, --file string               Input file (YAML/JSON), or '-' for stdin; mutually exclusive with flags
  -h, --help                      help for upsert
      --jq string                 jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string               Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --name string               Entity name (required)
  -o, --output string             Output format. One of: agents, json, table, yaml (default "json")
      --property stringToString   Property as key=value (repeatable or comma-separated) (default [])
      --scope stringToString      Scope as key=value (repeatable or comma-separated; identity-significant) (default [])
      --ttl string                Time-to-live duration (e.g. 1h, 7d); omitted = never expire
      --type string               Entity type (identifier; required)
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

* [gcx kg entities](gcx_kg_entities.md)	 - Manage Knowledge Graph entities.

