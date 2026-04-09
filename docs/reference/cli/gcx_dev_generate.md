## gcx dev generate

Generate typed Go stubs for Grafana resources

### Synopsis

Generate typed Go code stubs using grafana-foundation-sdk builder types.

The resource type is inferred from the immediate parent directory name:
  dashboards/  → dashboard
  alerts/      → alertrule
  alertrules/  → alertrule

The resource name is inferred from the filename (without .go extension).
Use --type to override type inference when the directory name does not match.

```
gcx dev generate [FILE_PATH]... [flags]
```

### Examples

```
  # Generate a dashboard stub
  gcx dev generate dashboards/my-service-overview.go

  # Generate an alert rule stub
  gcx dev generate alerts/high-cpu-usage.go

  # Generate multiple stubs at once
  gcx dev generate dashboards/a.go dashboards/b.go alerts/c.go

  # Override type inference with --type
  gcx dev generate internal/monitoring/cpu-alert.go --type alertrule
```

### Options

```
  -h, --help          help for generate
  -t, --type string   Resource type to generate (dashboard, alertrule). Overrides directory-based inference.
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

