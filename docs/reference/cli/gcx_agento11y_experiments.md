## gcx agento11y experiments

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

* [gcx agento11y](gcx_agento11y.md)	 - Manage Grafana Agent Observability resources
* [gcx agento11y experiments cancel](gcx_agento11y_experiments_cancel.md)	 - Cancel a running experiment.
* [gcx agento11y experiments create](gcx_agento11y_experiments_create.md)	 - Create a new experiment from a JSON or YAML file.
* [gcx agento11y experiments get](gcx_agento11y_experiments_get.md)	 - Get a single experiment by run ID.
* [gcx agento11y experiments get-report](gcx_agento11y_experiments_get-report.md)	 - Get the aggregate report for an experiment.
* [gcx agento11y experiments list](gcx_agento11y_experiments_list.md)	 - List experiments.
* [gcx agento11y experiments list-scores](gcx_agento11y_experiments_list-scores.md)	 - List scores produced by an experiment.
* [gcx agento11y experiments list-trials](gcx_agento11y_experiments_list-trials.md)	 - List test case trials for an experiment.
* [gcx agento11y experiments test-suites](gcx_agento11y_experiments_test-suites.md)	 - Manage experiment test suites.
* [gcx agento11y experiments trials](gcx_agento11y_experiments_trials.md)	 - Manage experiment test case trials.
* [gcx agento11y experiments update](gcx_agento11y_experiments_update.md)	 - Update an experiment's mutable fields.

