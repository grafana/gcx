## gcx dev scaffold

Scaffold a new Grafana resources-as-code project

### Synopsis

Scaffold a new Go project pre-configured for managing Grafana resources as code. Generates a module with example dashboards, a deploy workflow, and gcx configuration.

```
gcx dev scaffold [flags]
```

### Examples

```

	# Interactive scaffolding (prompts for project name and Go module path):
	gcx dev scaffold

	# Non-interactive with flags:
	gcx dev scaffold --project my-dashboards --go-module-path github.com/example/my-dashboards

```

### Options

```
      --go-module-path string   Go module path.
  -h, --help                    help for scaffold
  -p, --project string          Project name.
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

* [gcx dev](gcx_dev.md)	 - Manage Grafana resources as code

