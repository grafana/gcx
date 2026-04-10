## gcx sigil evaluators

Manage evaluator definitions (LLM judge, regex, heuristic).

### Options

```
  -h, --help   help for evaluators
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

* [gcx sigil](gcx_sigil.md)	 - Manage Sigil AI observability resources
* [gcx sigil evaluators create](gcx_sigil_evaluators_create.md)	 - Create or update an evaluator from a file.
* [gcx sigil evaluators delete](gcx_sigil_evaluators_delete.md)	 - Delete evaluators.
* [gcx sigil evaluators get](gcx_sigil_evaluators_get.md)	 - Get a single evaluator definition.
* [gcx sigil evaluators list](gcx_sigil_evaluators_list.md)	 - List evaluator definitions.
* [gcx sigil evaluators test](gcx_sigil_evaluators_test.md)	 - Run an evaluator against a generation without persisting results.

