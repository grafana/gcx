## gcx kg suppressions push

Push (create or update) one or more suppressions from a YAML file or stdin.

### Synopsis

Push (create or update) one or more suppressions from a YAML file or stdin.

Applies the entries in the input file, creating each suppression when absent or
updating it when present. Remote suppressions absent from the file are never
deleted. Use --dry-run to validate against the backend and preview the diff,
scoped to the entries in the input file, without uploading.

```
gcx kg suppressions push [flags]
```

### Examples

```
  gcx kg suppressions push -f suppressions.yaml

  # Validate against the backend and preview the diff without uploading:
  gcx kg suppressions push -f suppressions.yaml --dry-run

  echo 'disabledAlertConfigs:
    - name: my-suppression
      matchLabels:
        alertname: ErrorRatioBreach
        job: my-service' | gcx kg suppressions push
```

### Options

```
      --dry-run         Validate against the backend and show a diff without uploading.
  -f, --file string     Input file (YAML), or '-' for stdin. Reads from stdin if omitted.
  -h, --help            help for push
      --jq string       jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: agents, json, text, yaml (default "text")
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

* [gcx kg suppressions](gcx_kg_suppressions.md)	 - Manage alert suppressions in the Knowledge Graph.

