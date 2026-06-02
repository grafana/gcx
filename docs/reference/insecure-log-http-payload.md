# `--insecure-log-http-payload`

`gcx --insecure-log-http-payload <command>` enables full HTTP request/response
body logging for the lifetime of the command. This flag is intended for local
debugging of authentication and API issues only.

## What gets logged

When this flag is set, every HTTP round-trip through
`RequestResponseLoggingRoundTripper` writes the full dump (via
`httputil.DumpRequest` / `httputil.DumpResponse`) to the debug log, including:

- **Authorization tokens** (`Authorization: Bearer …`, `X-Grafana-Token: …`)
- **Cookies** (session tokens, CSRF tokens)
- **OAuth refresh tokens** (`oauth-refresh-token`, `gar_…` values)
- **Request and response bodies** (JSON payloads that may embed credentials)

## Why redaction is intentional

gcx engineers debugging authentication flows need to verify that the correct
token is being sent and that the server is returning the expected response.
Redacting credentials would defeat the flag's purpose. The flag's name
(`--insecure-log-http-payload`) is a deliberate signal that enabling it
produces sensitive output.

## Recommended workflow

1. Run `gcx --insecure-log-http-payload -v <command>` locally.
2. Inspect the log output in your terminal.
3. Do **not** pipe the output to a file and share it, paste it into a Slack
   channel, or attach it to a GitHub issue without first redacting all token
   values.
4. Rotate any credentials that appeared in logs you shared externally.

## Startup warning

When `--insecure-log-http-payload` is active, gcx prints a one-line warning
to stderr before any HTTP traffic flows:

```
WARNING: --insecure-log-http-payload is set. Authorization tokens, cookies,
OAuth refresh tokens, and request bodies will be written to debug logs.
Do not share or ship these logs.
```

## Migration from `--log-http-payload`

The flag was renamed to make the risk explicit. Using the old name
`--log-http-payload` now exits with an error:

```
unknown flag: --log-http-payload has been renamed; use --insecure-log-http-payload instead
```

The payload-dump behavior is unchanged — only the flag name and the startup
warning are new.
