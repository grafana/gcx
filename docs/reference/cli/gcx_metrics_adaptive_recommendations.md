## gcx metrics adaptive recommendations

Manage metric recommendations.

### Options

```
  -h, --help   help for recommendations
```

### Options inherited from parent commands

```
      --agent              Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, or GCX_AGENT_MODE env vars.
      --config string      Path to the configuration file to use
      --context string     Name of the context to use
      --limit int          Maximum number of items to return from list operations (0 for all; defaults to 50 in agent mode)
      --log-http-payload   Log full HTTP request/response bodies (includes headers — may expose tokens)
      --no-color           Disable color output
      --no-truncate        Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count      Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx metrics adaptive](gcx_metrics_adaptive.md)	 - Manage Adaptive Metrics resources
* [gcx metrics adaptive recommendations apply](gcx_metrics_adaptive_recommendations_apply.md)	 - Apply specific or all recommendations as rules.
* [gcx metrics adaptive recommendations diff](gcx_metrics_adaptive_recommendations_diff.md)	 - Show what applying recommendation(s) would change.
* [gcx metrics adaptive recommendations show](gcx_metrics_adaptive_recommendations_show.md)	 - Show metric recommendations.

