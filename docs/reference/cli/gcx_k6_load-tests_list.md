## gcx k6 load-tests list

List k6 Cloud load tests.

### Synopsis

List load tests from k6 Cloud. With --project-id, the server returns tests from that project only. Without it, the command lists tests across projects.

```
gcx k6 load-tests list [flags]
```

### Options

```
  -h, --help             help for list
      --jq string        jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string      Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int        Maximum number of items to return (0 for all) (default 50)
  -o, --output string    Output format. One of: agents, json, table, wide, yaml (default "table")
      --project-id int   Filter by project ID
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx k6 load-tests](gcx_k6_load-tests.md)	 - Manage k6 Cloud load tests.

