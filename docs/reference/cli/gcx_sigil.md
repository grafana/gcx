## gcx sigil

Manage Sigil AI observability resources

### Options

```
      --config string    Path to the configuration file to use
      --context string   Name of the context to use
  -h, --help             help for sigil
```

### Options inherited from parent commands

```
      --agent           Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --no-color        Disable color output
      --no-truncate     Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count   Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx](gcx.md)	 - Control plane for Grafana Cloud operations
* [gcx sigil agents](gcx_sigil_agents.md)	 - Query Sigil agent catalog.
* [gcx sigil conversations](gcx_sigil_conversations.md)	 - Query Sigil conversations.
* [gcx sigil evaluators](gcx_sigil_evaluators.md)	 - Manage evaluator definitions (LLM judge, regex, heuristic).
* [gcx sigil generations](gcx_sigil_generations.md)	 - Inspect individual LLM generations.
* [gcx sigil judge](gcx_sigil_judge.md)	 - List LLM providers and models available for LLM-judge evaluators.
* [gcx sigil rules](gcx_sigil_rules.md)	 - Manage rules that route generations to evaluators.
* [gcx sigil scores](gcx_sigil_scores.md)	 - View evaluation scores for generations.
* [gcx sigil templates](gcx_sigil_templates.md)	 - Browse reusable evaluator blueprints (global and tenant-scoped).

