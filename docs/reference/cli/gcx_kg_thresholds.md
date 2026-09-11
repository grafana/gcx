## gcx kg thresholds

Manage Knowledge Graph threshold rules.

### Synopsis

Read Asserts threshold rules over the v1 threshold config API.

This targets v1 (/v1/config/threshold-rules) — the version the Asserts app UI and all
user-configured thresholds run on.

### Options

```
  -h, --help   help for thresholds
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

* [gcx kg](gcx_kg.md)	 - Manage Grafana Knowledge Graph rules, entities, and insights
* [gcx kg thresholds get](gcx_kg_thresholds_get.md)	 - Get the whole threshold config as YAML.
* [gcx kg thresholds list](gcx_kg_thresholds_list.md)	 - List threshold rules for a category (request or resource).

