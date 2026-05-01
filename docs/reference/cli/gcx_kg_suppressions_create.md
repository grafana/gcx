## gcx kg suppressions create

Create or update one or more suppressions from a YAML file or stdin.

```
gcx kg suppressions create [flags]
```

### Examples

```
  gcx kg suppressions create -f suppressions.yaml

  echo 'disabledAlertConfigs:
    - name: my-suppression
      matchLabels:
        alertname: ErrorRatioBreach
        job: my-service' | gcx kg suppressions create
```

### Options

```
  -f, --file string   Input file (YAML), or '-' for stdin. Reads from stdin if omitted.
  -h, --help          help for create
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx kg suppressions](gcx_kg_suppressions.md)	 - Manage alert suppressions in the Knowledge Graph.

