## gcx agento11y

Manage Grafana Agent Observability resources

### Options

```
      --config string   Path to the configuration file to use
  -h, --help            help for agento11y
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx agento11y agents](gcx_agento11y_agents.md)	 - Query Agent Observability agent catalog.
* [gcx agento11y collections](gcx_agento11y_collections.md)	 - Manage named groups of saved conversations.
* [gcx agento11y conversations](gcx_agento11y_conversations.md)	 - Query Agent Observability conversations.
* [gcx agento11y evaluators](gcx_agento11y_evaluators.md)	 - Manage evaluator definitions (LLM judge, regex, heuristic).
* [gcx agento11y experiments](gcx_agento11y_experiments.md)	 - Manage eval experiment runs.
* [gcx agento11y generations](gcx_agento11y_generations.md)	 - Inspect individual LLM generations.
* [gcx agento11y guards](gcx_agento11y_guards.md)	 - Manage synchronous policy guards (hook rules) that evaluate generations on the request path.
* [gcx agento11y judge](gcx_agento11y_judge.md)	 - List LLM providers and models available for LLM-judge evaluators.
* [gcx agento11y rules](gcx_agento11y_rules.md)	 - Manage rules that route generations to evaluators.
* [gcx agento11y saved-conversations](gcx_agento11y_saved-conversations.md)	 - Bookmark live conversations as fixed inputs for evaluation runs.
* [gcx agento11y scores](gcx_agento11y_scores.md)	 - View evaluation scores for generations.
* [gcx agento11y templates](gcx_agento11y_templates.md)	 - Browse reusable evaluator blueprints (global and tenant-scoped).

