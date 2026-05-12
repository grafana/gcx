## gcx aio11y collections create

Create a new collection.

```
gcx aio11y collections create [flags]
```

### Examples

```
  # Create with inline flags.
  gcx aio11y collections create --name "Regression suite" --description "Nightly regression"

  # Create from a YAML file (either raw {name,description} or a typed resource envelope).
  gcx aio11y collections create -f collection.yaml
```

### Options

```
      --description string   Collection description
  -f, --filename string      File containing the collection create payload (use - for stdin)
  -h, --help                 help for create
      --json string          Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --name string          Collection name (required if --filename is not given)
  -o, --output string        Output format. One of: agents, json, yaml (default "json")
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use (overrides current-context in config)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx aio11y collections](gcx_aio11y_collections.md)	 - Manage named groups of saved conversations.

