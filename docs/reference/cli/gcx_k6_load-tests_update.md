## gcx k6 load-tests update

Update a k6 load test from a file.

```
gcx k6 load-tests update <id> [flags]
```

### Options

```
  -f, --filename string   File containing the test definition (JSON/YAML)
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

* [gcx k6 load-tests](gcx_k6_load-tests.md)	 - Manage k6 Cloud load tests.

