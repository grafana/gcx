## gcx faro apps update

Update a Faro app from a file.

```
gcx faro apps update <name> [flags]
```

### Examples

```
  # Update a Faro app using its slug-id.
  gcx faro apps update my-web-app-42 -f app.yaml
```

### Options

```
  -f, --filename string   File containing the Faro app manifest (use - for stdin)
  -h, --help              help for update
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx faro apps](gcx_faro_apps.md)	 - Manage Faro apps.

