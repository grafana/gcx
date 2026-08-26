# Edge-proxy authentication

Some Grafana instances sit behind an edge proxy that requires its own
credentials before a request reaches Grafana itself. A common example is an
AWS Application Load Balancer (ALB) configured with an Okta OIDC
authenticator: the ALB validates a session cookie on every request and
redirects unauthenticated traffic to the Okta login page, regardless of
whether the request carries a valid Grafana service-account token.

The `extra-headers` config field handles this. It stamps arbitrary HTTP
headers onto every outgoing request, outermost in the transport chain, so
the edge proxy sees them before any Grafana auth is evaluated.

## When to use this

Use `extra-headers` when:

- Your Grafana server URL returns an Okta (or other IdP) login page instead
  of a Grafana API response when you call it without a browser session.
- Grafana is behind an AWS ALB with an OIDC authenticator, a Cloudflare
  Access policy, or a similar session-cookie-gated proxy.
- A standard `gcx login` or `--token` flow fails because the proxy blocks the
  request before Grafana processes it.

Do **not** use `extra-headers` as a substitute for Grafana authentication.
You still need a valid Grafana credential (`auth-method: token`, `oauth`,
`basic`, or `mtls`) alongside the proxy header.

## AWS ALB + Okta setup

### Step 1: Get the session cookie

Sign in to the Grafana instance through your browser. The ALB sets a session
cookie named `AWSELBAuthSessionCookie-0` on the Grafana domain after you
complete the Okta flow.

To copy the cookie value:

1. Open the browser's DevTools (F12) and go to **Application > Cookies**.
2. Select the Grafana domain (e.g. `grafana.example.com`).
3. Copy the **Value** of `AWSELBAuthSessionCookie-0`.

The cookie typically expires after 12 hours. You will need to repeat this step
when it expires.

### Step 2: Add a stack entry with extra-headers

Edit `~/.config/gcx/config.yaml` and add `extra-headers` to the stack's
`grafana` block:

```yaml
stacks:
  my-grafana:
    grafana:
      server: https://grafana.example.com
      token: <your-grafana-service-account-token>
      auth-method: token
      org-id: 1
      extra-headers:
        Cookie: "AWSELBAuthSessionCookie-0=<value>"
```

Or create the context from scratch:

```bash
gcx config set stacks.my-grafana.grafana.server https://grafana.example.com
gcx config set stacks.my-grafana.grafana.auth-method token
gcx config set stacks.my-grafana.grafana.token <service-account-token>
gcx config set stacks.my-grafana.grafana.org-id 1
gcx config set 'stacks.my-grafana.grafana.extra-headers.Cookie' \
  'AWSELBAuthSessionCookie-0=<value>'
gcx config set contexts.my-grafana.stack my-grafana
gcx use my-grafana
```

### Step 3: Verify

```bash
gcx datasources list
```

If the cookie is valid, gcx returns your datasource list. If the ALB
redirects to Okta instead, the response will be HTML rather than JSON and
gcx will report a parse error — refresh the cookie (Step 1) and update the
config value.

## Refreshing the cookie

ALB session cookies expire. When they do, re-authenticate in your browser,
copy the new `AWSELBAuthSessionCookie-0` value, and update the config:

```bash
gcx config set 'stacks.my-grafana.grafana.extra-headers.Cookie' \
  'AWSELBAuthSessionCookie-0=<new-value>'
```

There is no automatic refresh. The cookie is an opaque value issued by the
ALB after it validates the Okta IdP response; gcx has no path to trigger the
Okta flow on your behalf.

## Security notes

- `extra-headers` values are marked `datapolicy:"secret"` and are redacted
  when you run `gcx config view`. They are stored in plaintext in
  `~/.config/gcx/config.yaml` — use filesystem permissions to restrict access
  to that file (`chmod 600 ~/.config/gcx/config.yaml`).
- ALB session cookies grant access equivalent to your browser session. Treat
  them with the same care as a password.
- If your organisation stores the cookie in a secrets manager, you can
  automate the update step with a short shell script rather than editing the
  config by hand.

## Other edge proxies

The same approach works for any proxy that accepts a header credential:

| Proxy | Typical header |
|---|---|
| AWS ALB OIDC | `Cookie: AWSELBAuthSessionCookie-0=<value>` |
| Cloudflare Access | `CF-Access-Token: <JWT>` |
| Custom internal proxy | Whatever header your proxy expects |

Set the header name and value under `extra-headers` in the stack config.
