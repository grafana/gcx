## gcx k6 load-zones create

Register a Private Load Zone.

```
gcx k6 load-zones create [flags]
```

### Options

```
      --cpu string           CPU limit for load zone pods (default "2")
  -h, --help                 help for create
      --image string         k6 runner image (default "grafana/k6:latest")
      --memory string        Memory limit for load zone pods (default "1Gi")
      --name string          Load zone name (must be unique in your org)
      --provider-id string   Provider ID for the load zone
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

* [gcx k6 load-zones](gcx_k6_load-zones.md)	 - Manage K6 private load zones.

