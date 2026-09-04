## gcx agento11y experiments export-conversations

Export an experiment's complete conversation source bundle to disk.

### Synopsis

Export the experiment record, report, paginated trial responses, and every
referenced conversation to a new directory. API response bodies are stored
without field selection or model-specific transformation so the bundle can be
curated into a fine-tuning dataset without losing source fields.

The destination must not already exist. Conversation requests run concurrently;
individual failures are recorded in the manifest and artifact receipt. Raw
conversation data may contain sensitive prompts, tool inputs, and tool outputs.

```
gcx agento11y experiments export-conversations <run-id> [flags]
```

### Examples

```
  # Export every conversation referenced by an experiment.
  gcx agento11y experiments export-conversations <run-id> -d ./exports/run-1

  # Reduce request pressure on the Agent Observability service.
  gcx agento11y experiments export-conversations <run-id> -d ./exports/run-1 --concurrency 4
```

### Options

```
      --concurrency int     Maximum number of concurrent conversation requests (default 10)
  -h, --help                help for export-conversations
  -d, --output-dir string   Directory to create for the export (required; must not already exist)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx agento11y experiments](gcx_agento11y_experiments.md)	 - Manage eval experiment runs.

