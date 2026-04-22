## gcx aio11y

Manage Grafana AI Observability resources

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for aio11y
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx aio11y agents](gcx_aio11y_agents.md)	 - Query AI Observability agent catalog.
* [gcx aio11y conversations](gcx_aio11y_conversations.md)	 - Query AI Observability conversations.
* [gcx aio11y evaluators](gcx_aio11y_evaluators.md)	 - Manage evaluator definitions (LLM judge, regex, heuristic).
* [gcx aio11y generations](gcx_aio11y_generations.md)	 - Inspect individual LLM generations.
* [gcx aio11y judge](gcx_aio11y_judge.md)	 - List LLM providers and models available for LLM-judge evaluators.
* [gcx aio11y rules](gcx_aio11y_rules.md)	 - Manage rules that route generations to evaluators.
* [gcx aio11y scores](gcx_aio11y_scores.md)	 - View evaluation scores for generations.
* [gcx aio11y templates](gcx_aio11y_templates.md)	 - Browse reusable evaluator blueprints (global and tenant-scoped).

