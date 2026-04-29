## gcx datasources pyroscope exemplars

Query profile or span exemplars from a Pyroscope datasource

### Synopsis

Query profile or span exemplars from a Pyroscope datasource.

Exemplars link profile data to concrete samples or trace spans, enabling a pivot from
"which service was slow" to "which exact profile" (profile exemplars) or "which trace
span" (span exemplars).

### Options

```
  -h, --help   help for exemplars
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

* [gcx datasources pyroscope](gcx_datasources_pyroscope.md)	 - Query Pyroscope datasources
* [gcx datasources pyroscope exemplars profile](gcx_datasources_pyroscope_exemplars_profile.md)	 - List individual profile exemplars
* [gcx datasources pyroscope exemplars span](gcx_datasources_pyroscope_exemplars_span.md)	 - List span exemplars (profiles linked to trace spans)

