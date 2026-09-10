## gcx agento11y experiments pull

[experimental] Pull an experiment's raw source bundle to disk.

### Synopsis

This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Pull the experiment record, aggregate report, and paginated trial responses to
a new directory. The report includes the evaluator scores and artifact metadata
returned for each trial. The trial index contains referenced conversation IDs,
but conversation payloads are not downloaded unless --include-conversations is
set. API response bodies are stored without field selection or model-specific
transformation so source fields remain available for offline analysis.

The destination must not already exist. Before downloading, gcx verifies that
the destination filesystem supports atomic no-replace directory publication.
When conversations are included, their requests run concurrently and individual
failures are recorded in the manifest and artifact receipt. Pulled data may
contain sensitive prompts, tool inputs, and tool outputs. Each bundle includes
an AGENTS.md with safe-handling instructions and a .gitignore that excludes the
entire bundle from Git by default.

```
gcx agento11y experiments pull <run-id> [flags]
```

### Examples

```
  # Pull experiment metadata, aggregate report, trials, and conversation IDs.
  gcx agento11y experiments pull <run-id> -d ./exports/run-1

  # Also download every referenced conversation with reduced request pressure.
  gcx agento11y experiments pull <run-id> -d ./exports/run-1 --include-conversations --concurrency 4
```

### Options

```
      --concurrency int         Maximum concurrent requests when including conversations (default 10)
  -h, --help                    help for pull
      --include-conversations   Download the full payload for every conversation referenced by a trial
  -d, --output-dir string       Directory to create for the pulled bundle (required; must not already exist)
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Requires -vvv. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx agento11y experiments](gcx_agento11y_experiments.md)	 - Manage eval experiment runs.

