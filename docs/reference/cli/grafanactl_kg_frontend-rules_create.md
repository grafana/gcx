## grafanactl kg frontend-rules create

Upload frontend observability rules from a file (YAML).

```
grafanactl kg frontend-rules create [flags]
```

### Options

```
  -f, --file string   Input file (YAML)
  -h, --help          help for create
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GRAFANACTL_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [grafanactl kg frontend-rules](grafanactl_kg_frontend-rules.md)	 - Push frontend observability rules.

