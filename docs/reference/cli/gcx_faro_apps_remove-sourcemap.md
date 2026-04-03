## gcx faro apps remove-sourcemap

Remove sourcemap bundles from a Faro app.

```
gcx faro apps remove-sourcemap <app-name> <bundle-id> [bundle-id...] [flags]
```

### Examples

```
  # Remove a single sourcemap bundle.
  gcx faro apps remove-sourcemap my-web-app-42 1234567890-abc12

  # Remove multiple bundles at once.
  gcx faro apps remove-sourcemap my-web-app-42 bundle-1 bundle-2 bundle-3
```

### Options

```
  -h, --help   help for remove-sourcemap
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

