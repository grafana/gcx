## gcx kg entities delete

Delete a custom entity [experimental].

### Synopsis

Delete an API-origin entity. Scope is part of the entity's identity, so it must
match the value used at create — omitting it targets the scope-less entity, and a
mismatch returns 404 (not found).

Experimental: this command uses the Knowledge Graph write API, which is gated
server-side and may change.

```
gcx kg entities delete [Type--Name] [flags]
```

### Options

```
      --domain string          Writable domain slug — a specific application domain such as 'irm' (required)
      --force                  Skip confirmation prompt
  -h, --help                   help for delete
      --name string            Entity name (or use positional Type--Name)
      --scope stringToString   Scope as key=value (repeatable or comma-separated; must match create-time scope) (default [])
      --type string            Entity type (or use positional Type--Name)
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

