## gcx profiles

Query Pyroscope datasources and manage continuous profiling

### Synopsis

Query Pyroscope datasources and manage continuous profiling.

Use 'list-profile-types' and 'labels' (or 'series') to find profile type IDs
and label values, 'metrics' to see how cost changes over time, and 'query' to
see which code paths account for it (-o dot for call graphs, -o pprof for local
analysis). An empty result means no matching samples, not zero cost. To
investigate a regression, compare against a baseline window of equal length
with the same selector and profile type.

### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for profiles
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from known agent identity variables. Set GCX_AGENT_NAME to identify a supported harness, or GCX_AGENT_MODE to control the mode.
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx profiles adaptive](gcx_profiles_adaptive.md)	 - Manage Adaptive Profiles (not yet available)
* [gcx profiles data-range](gcx_profiles_data-range.md)	 - Show the range of profiling data the datasource holds
* [gcx profiles exemplars](gcx_profiles_exemplars.md)	 - Query profile or span exemplars from a Pyroscope datasource
* [gcx profiles labels](gcx_profiles_labels.md)	 - List labels or label values
* [gcx profiles list-profile-types](gcx_profiles_list-profile-types.md)	 - List available profile types
* [gcx profiles metrics](gcx_profiles_metrics.md)	 - Query profile time-series data from a Pyroscope datasource
* [gcx profiles query](gcx_profiles_query.md)	 - Execute a profiling query against a Pyroscope datasource
* [gcx profiles series](gcx_profiles_series.md)	 - List unique profile label sets

