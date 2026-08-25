# `--insecure-log-http-payload`

`gcx --insecure-log-http-payload <command>` enables full HTTP request/response
body logging for the lifetime of the command. This flag is intended for local
debugging of authentication and API issues only.

## What gets logged

When this flag is set, every HTTP round-trip through
`RequestResponseLoggingRoundTripper` writes the full dump (via
`httputil.DumpRequestOut` / `httputil.DumpResponse`) to the debug log, including:

- **Authorization tokens** (`Authorization: Bearer …`, `X-Grafana-Token: …`)
- **Cookies** (session tokens, CSRF tokens)
- **OAuth refresh tokens** (`oauth-refresh-token`, `gar_…` values)
- **Request and response bodies** (JSON payloads that may embed credentials)

The dump is the innermost transport layer. It therefore shows every header that
reaches the wire, including the bearer token that the OAuth transport adds, the
`X-Grafana-Caller-Id` header, and the user agent.

## How to find the dump

The dumps log at **Debug** level, so they need `-vvv`. Each dump carries a
label, because a wire dump holds no word that identifies it:

- `http request dump`
- `http response dump`

Search for the label, not for the word "body":

```bash
gcx --insecure-log-http-payload -vvv <command> 2>&1 | grep -A20 "http request dump"
```

A dump looks like this. The request line, the headers, and the raw body follow
the label:

```
DEBUG http request dump
POST /api/plugins/grafana-irm-app/resources/oncall/…/schedules/ HTTP/1.1
Host: example.grafana.net
Authorization: Bearer <token>
Content-Type: application/json
Content-Length: 42

{"name":"primary-rotation","type":"calendar"}
```

No response dump appears when the round trip fails, because there is no
response to dump. The `WARN http error` line carries the reason.

The dump also covers the OAuth token refresh exchange, because
`auth.RefreshTransport` sends the refresh request through the same inner layer.
Those dumps carry the refresh token and, on success, the rotated token pair.
When a refresh fails, gcx never sends your original request, so only the refresh
exchange appears. The `WARN http error` line still carries the reason. Log in
again, then repeat the command:

```
WARN http error method=GET url=https://… error="token refresh failed: session expired"
```

## Why redaction is intentional

gcx engineers debugging authentication flows need to verify that the correct
token is being sent and that the server is returning the expected response.
Redacting credentials would defeat the flag's purpose. The flag's name
(`--insecure-log-http-payload`) is a deliberate signal that enabling it
produces sensitive output.

## Recommended workflow

1. Run `gcx --insecure-log-http-payload -vvv <command>` locally.
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

The rename itself did not change the dump. It changed the flag name and added
the startup warning.
