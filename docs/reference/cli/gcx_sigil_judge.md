## gcx sigil judge

List LLM providers and models available for LLM-judge evaluators.

### Synopsis

List LLM providers and models available for LLM-judge evaluators.

Use these values in the 'provider' and 'model' fields of an llm_judge evaluator config.

### Options

```
  -h, --help   help for judge
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

* [gcx sigil](gcx_sigil.md)	 - Manage Sigil AI observability resources
* [gcx sigil judge models](gcx_sigil_judge_models.md)	 - List available judge models.
* [gcx sigil judge providers](gcx_sigil_judge_providers.md)	 - List available judge providers.

