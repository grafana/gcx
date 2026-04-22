## gcx aio11y templates get

Get a single eval template.

### Synopsis

Get the full template definition including config and output keys.

Templates are reusable evaluator blueprints. Export a template as YAML,
customize it, and create an evaluator with 'evaluators create -f'.

```
gcx aio11y templates get <template-id> [flags]
```

### Examples

```
  # Get a template's config and output keys.
  gcx aio11y templates get my-template -o yaml > evaluator.yaml
  gcx aio11y evaluators create -f evaluator.yaml
```

### Options

```
  -h, --help            help for get
      --json string     Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
  -o, --output string   Output format. One of: json, yaml (default "yaml")
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

* [gcx aio11y templates](gcx_aio11y_templates.md)	 - Browse reusable evaluator blueprints (global and tenant-scoped).

