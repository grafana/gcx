## gcx setup instrumentation apply

Apply an InstrumentationConfig manifest.

```
gcx setup instrumentation apply [flags]
```

### Options

```
      --dry-run           Preview changes without applying
  -f, --filename string   Path to the InstrumentationConfig manifest file (required)
  -h, --help              help for apply
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

* [gcx setup instrumentation](gcx_setup_instrumentation.md)	 - Manage observability instrumentation for Kubernetes clusters.

