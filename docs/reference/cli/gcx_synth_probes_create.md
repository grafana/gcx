## gcx synth probes create

Create a Synthetic Monitoring probe.

```
gcx synth probes create [flags]
```

### Examples

```
  # Create a probe with a name and region.
  gcx synth probes create --name my-probe --region eu

  # Create a probe with labels and coordinates.
  gcx synth probes create --name my-probe --region us --labels env=prod,team=sre --latitude 37.7749 --longitude -122.4194
```

### Options

```
  -h, --help              help for create
      --labels strings    Labels in key=value format
      --latitude float    Probe latitude
      --longitude float   Probe longitude
      --name string       Probe name (required)
      --region string     Probe region
```

### Options inherited from parent commands

```
      --agent            Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
      --no-color         Disable color output
      --no-truncate      Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count    Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx synth probes](gcx_synth_probes.md)	 - Manage Synthetic Monitoring probes.

