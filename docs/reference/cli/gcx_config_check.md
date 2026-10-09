## gcx config check

Check configuration contexts for issues

### Synopsis

Check configured contexts for issues.

Without --context, checks every configured context. With --context, checks only the selected context.

```
gcx config check [flags]
```

### Examples

```

	gcx config check
	gcx config check --context production
```

### Options

```
  -h, --help   help for check
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx config](gcx_config.md)	 - View or manipulate configuration settings

