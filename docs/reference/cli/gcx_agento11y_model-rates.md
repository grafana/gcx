## gcx agento11y model-rates

Configure your own negotiated model prices.

### Synopsis

Configure your own negotiated model prices.

Cost figures are otherwise computed from a public catalog, so a negotiated
rate, a committed-use discount or a reseller agreement shows a price that is
not yours — and a model the catalog does not carry shows no price at all.

Rates are given in USD per million tokens, the same way providers publish
them, so what you type is what you can check against your contract.

Setting a rate applies it from that moment on. Generations already recorded
keep the price they were given, and a later change records a new rate rather
than replacing the old one, so history stays readable.

### Options

```
  -h, --help   help for model-rates
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

* [gcx agento11y](gcx_agento11y.md)	 - Manage Grafana Agent Observability resources
* [gcx agento11y model-rates create](gcx_agento11y_model-rates_create.md)	 - Record your price for one model, in force from now.
* [gcx agento11y model-rates delete](gcx_agento11y_model-rates_delete.md)	 - Delete one configured rate.
* [gcx agento11y model-rates list](gcx_agento11y_model-rates_list.md)	 - List the rates you have configured.

