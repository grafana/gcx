## gcx agento11y model-rates create

Record your price for one model, in force from now.

### Synopsis

Record your price for one model, in force from now.

The rate replaces the public catalog price for this provider and model
completely: nothing is filled in from the catalog card, so state every rate
your contract covers.

A bucket you leave unset is not charged, and passing 0 charges the same. The
difference is what it records: 0 says your contract prices that bucket at
nothing, while leaving the flag off says nothing about it at all. At least one
rate has to be set, and an explicit 0 counts.

The long-context flags do not work that way. They are changes on top of the
rates above, so a bucket you leave unset there keeps charging its base rate
above the threshold, and only an explicit 0 makes it free. Omitting
--long-context-price-input is not the same as setting it to 0.

Generations already recorded keep the price they were given. A later call
records a new rate rather than overwriting this one.

```
gcx agento11y model-rates create [flags]
```

### Examples

```
  # A flat negotiated rate.
  gcx agento11y model-rates create --provider openai --model gpt-5.5 \
      --price-input 2.00 --price-output 8.00

  # A model the catalog does not carry.
  gcx agento11y model-rates create --provider acme --model in-house-7b \
      --price-input 1.00 --price-output 4.00

  # A contract that keeps the provider's long-context tier.
  gcx agento11y model-rates create --provider openai --model gpt-5.5 \
      --price-input 2.00 --price-output 8.00 \
      --long-context-threshold 272000 \
      --long-context-price-input 4.00 --long-context-price-output 12.00
```

### Options

```
  -h, --help                                   help for create
      --jq string                              jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string                            Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --long-context-price-cache-read float    USD per million cache-read tokens above the threshold. Omit to keep --price-cache-read above it; pass 0 to charge nothing
      --long-context-price-cache-write float   USD per million cache-write tokens above the threshold. Omit to keep --price-cache-write above it; pass 0 to charge nothing
      --long-context-price-input float         USD per million input tokens above the threshold. Omit to keep --price-input above it; pass 0 to charge nothing
      --long-context-price-output float        USD per million output tokens above the threshold. Omit to keep --price-output above it; pass 0 to charge nothing
      --long-context-threshold int             Input tokens above which the long-context rates apply
      --model string                           Model the rate applies to, as your telemetry reports it (required)
  -o, --output string                          Output format. One of: agents, json, yaml (default "yaml")
      --price-cache-read float                 USD per million tokens read from the prompt cache
      --price-cache-write float                USD per million tokens written to the prompt cache
      --price-input float                      USD per million input tokens
      --price-output float                     USD per million output tokens
      --price-request float                    USD charged per call, for models that have a flat fee
      --provider string                        Provider the model belongs to, as your telemetry reports it (required)
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

* [gcx agento11y model-rates](gcx_agento11y_model-rates.md)	 - Configure your own negotiated model prices.

