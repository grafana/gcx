## gcx annotations

Manage Grafana annotations

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for annotations
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx annotations create](gcx_annotations_create.md)	 - Create an annotation from a JSON file.
* [gcx annotations delete](gcx_annotations_delete.md)	 - Delete an annotation by ID.
* [gcx annotations get](gcx_annotations_get.md)	 - Get an annotation by ID.
* [gcx annotations list](gcx_annotations_list.md)	 - List annotations (last 24h by default).
* [gcx annotations update](gcx_annotations_update.md)	 - Update an annotation from a JSON file (PATCH).

