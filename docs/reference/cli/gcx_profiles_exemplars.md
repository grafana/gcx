## gcx profiles exemplars

Query profile or span exemplars from a Pyroscope datasource

### Synopsis

Query profile or span exemplars from a Pyroscope datasource.

Exemplars link profile data to concrete samples or trace spans, enabling a pivot from
"which service was slow" to "which exact profile" (profile exemplars) or "which trace
span" (span exemplars).

### Examples

```

  # Top individual profile exemplars (Profile ID + Span ID if span-aware)
  gcx profiles exemplars profile '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # Top span exemplars (profiles linked to trace spans; needs otelpyroscope)
  gcx profiles exemplars span '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --since 1h

  # Output as JSON for scripting
  gcx profiles exemplars profile '{}' --since 30m -o json
```

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

* [gcx profiles](gcx_profiles.md)	 - Query Pyroscope datasources and manage continuous profiling
* [gcx profiles exemplars profile](gcx_profiles_exemplars_profile.md)	 - List individual profile exemplars
* [gcx profiles exemplars span](gcx_profiles_exemplars_span.md)	 - List span exemplars (profiles linked to trace spans)

