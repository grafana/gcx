## gcx annotations update

Update an annotation from a JSON file (PATCH).

```
gcx annotations update ID [flags]
```

### Examples

```
  gcx annotations update 1 -f patch.json
```

### Options

```
  -f, --file string   JSON file containing the patch (use - for stdin)
  -h, --help          help for update
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

* [gcx annotations](gcx_annotations.md)	 - Manage Grafana annotations

