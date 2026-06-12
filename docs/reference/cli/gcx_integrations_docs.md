## gcx integrations docs

Show installation docs and prerequisites for an integration.

### Synopsis

Show the prerequisites and installation steps for a Grafana Cloud integration, fetched from the public documentation. By default the advanced configuration and reference sections are omitted; use --full to see the entire page, or --prerequisites / --install / --config / --advanced / --kubernetes to print only that section.

```
gcx integrations docs <slug> [flags]
```

### Options

```
      --advanced        Show only the advanced-mode Grafana Alloy configuration snippets
      --config          Show only the simple-mode Grafana Alloy configuration snippets
      --full            Show the full page, including advanced configuration and reference sections
  -h, --help            help for docs
      --install         Show only the installation steps section
      --kubernetes      Show only the Kubernetes installation instructions section
      --open            Open the documentation in your browser instead of printing it
      --prerequisites   Show only the prerequisites ("Before you begin") section
      --raw             Print raw Markdown without terminal styling
      --url             Print only the documentation URL
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx integrations](gcx_integrations.md)	 - List available Grafana Cloud integrations

