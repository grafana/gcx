## gcx frontend apps create

Create a Frontend Observability app from a file.

```
gcx frontend apps create [flags]
```

### Examples

```
  # Create an app from a YAML file.
  gcx frontend apps create -f app.yaml

  # Create from stdin.
  cat app.yaml | gcx frontend apps create -f -
```

### Options

```
  -f, --filename string   File containing the Frontend Observability app manifest (use - for stdin)
  -h, --help              help for create
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

* [gcx frontend apps](gcx_frontend_apps.md)	 - Manage Frontend Observability apps.

