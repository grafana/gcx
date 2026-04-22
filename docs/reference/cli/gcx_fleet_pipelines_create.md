## gcx fleet pipelines create

Create a pipeline from a file.

```
gcx fleet pipelines create [flags]
```

### Options

```
  -f, --filename string   File containing the pipeline manifest (use - for stdin)
      --force             Override protection guard for instrumentation-managed pipelines
  -h, --help              help for create
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx fleet pipelines](gcx_fleet_pipelines.md)	 - Manage Fleet Management pipelines.

