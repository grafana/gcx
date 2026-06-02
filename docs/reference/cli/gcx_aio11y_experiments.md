## gcx aio11y experiments

Manage eval experiment runs.

### Options

```
  -h, --help   help for experiments
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

* [gcx aio11y](gcx_aio11y.md)	 - Manage Grafana AI Observability resources
* [gcx aio11y experiments cancel](gcx_aio11y_experiments_cancel.md)	 - Cancel a running experiment.
* [gcx aio11y experiments create](gcx_aio11y_experiments_create.md)	 - Create a new experiment from a JSON or YAML file.
* [gcx aio11y experiments get](gcx_aio11y_experiments_get.md)	 - Get a single experiment by run ID.
* [gcx aio11y experiments list](gcx_aio11y_experiments_list.md)	 - List experiments.
* [gcx aio11y experiments report](gcx_aio11y_experiments_report.md)	 - Fetch the aggregate report for an experiment.
* [gcx aio11y experiments scores](gcx_aio11y_experiments_scores.md)	 - List scores produced by an experiment.
* [gcx aio11y experiments update](gcx_aio11y_experiments_update.md)	 - Patch an experiment's mutable fields.

