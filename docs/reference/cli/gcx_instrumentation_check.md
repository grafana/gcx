## gcx instrumentation check

Validate OpenTelemetry instrumentation for an application

### Synopsis

Validate OpenTelemetry instrumentation configuration for an application
running locally.

Checks performed:
  - Common OTEL_* environment variables (resource attributes, exporter, etc.)
  - SDK setup and dependencies for the chosen language
  - OpenTelemetry Collector config file (YAML schema, pipelines, exporters)
  - Grafana Beyla configuration
  - Grafana Alloy configuration
  - Grafana Cloud connectivity (uses env vars for endpoint and credentials)

Components is an optional comma-separated list — defaults to all when omitted.
Supported components: sdk, beyla, alloy, collector, grafana-cloud.

Powered by github.com/grafana/otel-checker.

```
gcx instrumentation check [components] [flags]
```

### Options

```
      --collector-config-path string   Path to the OpenTelemetry Collector config file.
      --debug                          Print additional diagnostic output from the checker.
  -h, --help                           help for check
      --instrumentation-file string    Path to the JS instrumentation file. Required when --language=js and --manual-instrumentation.
      --jq string                      jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string                    Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --language string                Application language. Required for sdk, beyla, alloy, grafana-cloud. Possible values: dotnet, go, java, js, python, ruby, php
      --manual-instrumentation         Application is using manual instrumentation (JS only).
  -o, --output string                  Output format. One of: agents, json, table, wide, yaml (default "table")
      --package-json-path string       Path to package.json for JS dependency checks.
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx instrumentation](gcx_instrumentation.md)	 - Manage Grafana Instrumentation Hub

