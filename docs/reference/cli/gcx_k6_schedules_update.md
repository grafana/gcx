## gcx k6 schedules update

Update a K6 schedule from a file.

```
gcx k6 schedules update <id> [flags]
```

### Options

```
  -f, --filename string   File containing the schedule request (JSON/YAML)
  -h, --help              help for update
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

* [gcx k6 schedules](gcx_k6_schedules.md)	 - Manage K6 Cloud schedules.

