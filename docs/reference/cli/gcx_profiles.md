## gcx profiles

Query Pyroscope datasources and manage continuous profiling

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for profiles
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx profiles adaptive](gcx_profiles_adaptive.md)	 - Manage Adaptive Profiles (not yet available)
* [gcx profiles exemplars](gcx_profiles_exemplars.md)	 - Query profile or span exemplars from a Pyroscope datasource
* [gcx profiles labels](gcx_profiles_labels.md)	 - List labels or label values
* [gcx profiles metrics](gcx_profiles_metrics.md)	 - Query profile time-series data from a Pyroscope datasource
* [gcx profiles profile-types](gcx_profiles_profile-types.md)	 - List available profile types
* [gcx profiles query](gcx_profiles_query.md)	 - Execute a profiling query against a Pyroscope datasource

