## gcx kg entities list

List entities by type/scope, or look up an entity's identity and properties.

### Synopsis

List Knowledge Graph entities for a given type, env, site, namespace.

The cheap default for listing entities or looking up basic information about
one: its identity (name, type, scope), properties (the labels used to build
PromQL/Loki queries — job, namespace, container, image, etc.), and a small
snapshot of its current insights. Filter to a single entity with
'--property name=<exact-name>'.

For an entity's full insight timeline or related entities for root-cause
analysis, use 'gcx kg entities inspect' instead.

```
gcx kg entities list [flags]
```

### Examples

```
  gcx kg entities list --type Service --env <env> --namespace <namespace> --property name=<service-name>
  gcx kg entities list --type Service --env <env> --insight any
  gcx kg entities list --type Service --env <env> --insight name=Saturation --insight severity=critical
  gcx kg entities list --type Service --env <env> --property name=~api --insight severity=critical
  gcx kg entities list --type Service --env <env> --insight severity=critical --json name,scope
```

### Options

```
      --env string             Environment scope (run 'gcx kg meta scopes' to see valid values)
      --from string            Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                   help for list
      --insight stringArray    Filter to entities with an active insight: 'any' (has any insight) or key=value (key=~value for substring; name only); valid keys: name, severity (critical|warning|info); repeatable — multiple predicates must match the same assertion
      --jq string              jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string            Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int              Maximum number of items to return (0 for all; the backend may still page results — use --page to paginate) (default 50)
      --namespace string       Namespace scope (run 'gcx kg meta scopes' to see valid values)
  -o, --output string          Output format. One of: agents, json, table, yaml (default "table")
      --page int               Page number (0-based)
      --property stringArray   Filter by property: name=value (exact) or name=~value (contains); repeatable (run 'gcx kg meta schema' to list property names)
      --since string           Duration before --to (or now); mutually exclusive with --from/--to (e.g. 1h, 30m, 7d)
      --site string            Site scope (run 'gcx kg meta scopes' to see valid values)
      --to string              End time (RFC3339, Unix timestamp, or relative like 'now')
      --type string            Entity type to list (run 'gcx kg meta schema' to see available types)
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

