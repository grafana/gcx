## gcx ext

Install and run third-party extensions

### Synopsis

Install, manage, and run third-party gcx extensions.

Extensions are not audited by Grafana. Anything you install runs with your full user permissions on this machine: review an extension's source and its publisher before installing it.

Arguments after an extension's name are passed to it verbatim, so gcx's own global flags must come before 'ext' (gcx --context prod ext my-ext --flag).

```
gcx ext [name] [args...] [flags]
```

### Examples

```
  # Install from a local checkout, a manifest URL, or any git remote
  gcx ext install ./my-extension
  gcx ext install https://example.com/my-extension/gcx-extension.yaml
  gcx ext install https://github.com/acme/gcx-ext-thing.git

  # Run it
  gcx ext my-extension --help
```

### Options

```
  -h, --help   help for ext
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx ext install](gcx_ext_install.md)	 - Install an extension from a local path, manifest URL, or git URL
* [gcx ext list](gcx_ext_list.md)	 - List installed extensions
* [gcx ext uninstall](gcx_ext_uninstall.md)	 - Remove an installed extension
* [gcx ext update](gcx_ext_update.md)	 - Reinstall extensions from the source they were installed from

