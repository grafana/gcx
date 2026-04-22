## gcx aio11y evaluators

Manage evaluator definitions (LLM judge, regex, heuristic).

### Options

```
  -h, --help   help for evaluators
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

* [gcx aio11y](gcx_aio11y.md)	 - Manage Grafana AI Observability resources
* [gcx aio11y evaluators create](gcx_aio11y_evaluators_create.md)	 - Create or update an evaluator from a file.
* [gcx aio11y evaluators delete](gcx_aio11y_evaluators_delete.md)	 - Delete evaluators.
* [gcx aio11y evaluators get](gcx_aio11y_evaluators_get.md)	 - Get a single evaluator definition.
* [gcx aio11y evaluators list](gcx_aio11y_evaluators_list.md)	 - List evaluator definitions.
* [gcx aio11y evaluators test](gcx_aio11y_evaluators_test.md)	 - Run an evaluator against a generation without persisting results.

