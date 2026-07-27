## gcx kg notifications upsert

Upsert (create or update) one or more alert notification configs from a YAML file or stdin.

### Synopsis

Upsert (create or update) one or more alert notification configs from a YAML file or stdin.

Applies the entries in the input file, creating each config when absent or
updating it when present. Remote configs absent from the file are never
deleted.

```
gcx kg notifications upsert [flags]
```

### Examples

```
  gcx kg notifications upsert -f notifications.yaml

  echo 'alertConfigs:
    - name: api-server-latency
      matchLabels:
        asserts_slo_name: api-server-latency
      for: 5m
      silenced: false' | gcx kg notifications upsert
```

### Options

```
  -f, --file string   Input file (YAML), or '-' for stdin. Reads from stdin if omitted.
  -h, --help          help for upsert
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

* [gcx kg notifications](gcx_kg_notifications.md)	 - Manage alert notification configs in the Knowledge Graph.

