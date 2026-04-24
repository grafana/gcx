# Authentication Subsystem

## Overview

gcx supports three authentication methods — browser-based OAuth (PKCE),
Grafana service account tokens, and Grafana Cloud Access Policy tokens — and
each targets a different API surface. The authentication subsystem spans
package boundaries: OAuth mechanics live in `internal/auth/`, token storage
lives in `internal/config/` as fields on `GrafanaConfig` and `CloudConfig`,
and token attachment to HTTP clients happens in `internal/config/rest.go`.
This document covers all three methods end to end.

```mermaid
graph LR
    OAuth[OAuth PKCE<br/>gat_ + gar_ tokens]
    SA[Service account token<br/>glsa_...]
    CAP[Cloud Access Policy token<br/>glc_...]

    GrafanaAPI[Grafana API<br/>instance-scoped]
    K8sAPI[K8s /apis<br/>instance-scoped]
    GCOM[GCOM API<br/>Cloud-wide]
    CloudProd[Cloud product APIs<br/>synth, k6, IRM, ...]

    OAuth --> GrafanaAPI
    OAuth --> K8sAPI
    SA --> GrafanaAPI
    SA --> K8sAPI
    CAP --> GCOM
    CAP --> CloudProd
```

---

## Auth methods at a glance

| Method | Target | Provisioning | Storage field | Refresh | Rotation |
|---|---|---|---|---|---|
| OAuth PKCE | Grafana API, K8s `/apis` | Browser flow via `gcx login` | `GrafanaConfig.OAuthToken`, `OAuthRefreshToken`, `OAuthTokenExpiresAt`, `OAuthRefreshExpiresAt`, `ProxyEndpoint` | Automatic via `RefreshTransport` | Transparent |
| Service account token | Grafana API, K8s `/apis` | Grafana UI → Administration → Service accounts | `GrafanaConfig.APIToken` | None (static) | Manual (rotate in Grafana UI) |
| Cloud Access Policy token | GCOM, Cloud product APIs | Grafana Cloud UI → Security → Access policies | `CloudConfig.Token` | None (static) | Manual (rotate in Cloud UI) |

---

## Service account tokens

Service account tokens are static bearer credentials issued by a Grafana
instance. Users provision them through the Grafana UI under Administration →
Service accounts; the token carries the scope of its account (Editor, Admin,
or custom role). Tokens are prefixed `glsa_`.

gcx stores them in `GrafanaConfig.APIToken` (`datapolicy:"secret"`, redacted
in `gcx config view`). The REST config builder in `internal/config/rest.go`
sets them as `rest.Config.BearerToken` when no OAuth credentials are present.

Rotation is manual: rotate in the Grafana UI, then update the context with
`gcx login --context X --token glsa_new_token`.

---

## Cloud Access Policy tokens

Cloud Access Policy tokens are static bearer credentials issued by Grafana
Cloud. Users provision them in the Cloud UI under Security → Access policies;
the token carries the scope of the policy (metrics read, logs write, IRM
admin, etc.). Tokens are prefixed `glc_`.

gcx stores them in `CloudConfig.Token` (`datapolicy:"secret"`). They are
attached to two different API surfaces: the GCOM API (via the
`internal/cloud/` client) and Cloud product APIs (via product-specific REST
clients for synth, k6, IRM, fleet, and others).

Rotation is manual: rotate the access policy in the Cloud UI, then update
the context with `gcx login --context X --cloud-token glc_new_token`.

---

## OAuth PKCE flow

OAuth PKCE is the default for interactive users on Grafana Cloud. The flow
spans three remote actors: the Grafana instance (which hosts the
`grafana-assistant-app` plugin and renders the consent UI), the assistant
backend (which issues and refreshes tokens), and a short-lived callback
server that gcx starts on a loopback port.

```mermaid
sequenceDiagram
    participant User
    participant gcx
    participant Callback as Local callback<br/>(127.0.0.1:PORT)
    participant Browser
    participant Grafana as Grafana instance<br/>(assistant-app plugin)
    participant Backend as Assistant backend

    gcx->>gcx: Generate state + PKCE<br/>code_verifier / code_challenge
    gcx->>Callback: Start on 127.0.0.1:PORT (54321-54399)
    gcx->>Browser: Open /a/grafana-assistant-app/cli/auth<br/>?callback_port&state&code_challenge
    Browser->>Grafana: User approves consent
    Grafana->>Browser: Redirect 127.0.0.1:PORT/callback<br/>?code&state&endpoint
    Browser->>Callback: GET /callback?code&state&endpoint
    Callback->>Callback: Validate state (CSRF)<br/>and endpoint (trusted domain)
    Callback->>Backend: POST endpoint/api/cli/v1/auth/exchange<br/>{code, code_verifier}
    Backend->>Callback: gat_ access token,<br/>gar_ refresh token, expiries
    Callback->>gcx: Deliver Result via channel
    gcx->>gcx: Persist tokens + ProxyEndpoint<br/>to GrafanaConfig
```

PKCE (Proof Key for Code Exchange, RFC 7636) protects against intercepted
authorization codes: the `code_verifier` never leaves gcx, so an attacker who
captures the authorization code cannot exchange it for a token without also
knowing the verifier. The callback binds to a loopback address (default
`127.0.0.1`) because browser vendors treat loopback URIs as secure contexts
for OAuth redirects without requiring custom URI schemes. The port is picked
from the range `54321-54399`.

Two extra safeguards sit inside the callback handler. The `state` parameter
is generated by gcx and re-checked on return, which closes the classic CSRF
hole. The `endpoint` query parameter — supplied by the assistant plugin and
used as the base URL for the token exchange — is validated by
`ValidateEndpointURL` against a short list of trusted Grafana suffixes
(`.grafana.net`, `.grafana-dev.net`, `.grafana-ops.net`) plus loopback. This
prevents an attacker who controls the browser redirect from steering the
token exchange at a hostile host.

The token exchange response carries an `api_endpoint` field, stored as
`GrafanaConfig.ProxyEndpoint`. All subsequent API traffic is routed through
that endpoint (see OAuth proxy routing below). Exact implementation lives in
`internal/auth/flow.go`.

---

## Token lifecycle

| Method | Lifecycle |
|---|---|
| OAuth PKCE | Dynamic. The `gat_` access token has a short expiry; the `gar_` refresh token has a longer one. `RefreshTransport` renews the access token when a request sees credentials inside the 5-minute refresh threshold (`refreshThreshold` in `internal/auth/transport.go`), and refresh-token rotation on successful refresh is persisted back to the config file. |
| Service account token | Static. Lives until manually rotated in the Grafana UI. gcx treats it as an opaque bearer credential. |
| Cloud Access Policy token | Static. Lives until manually rotated in the Grafana Cloud UI. |

All token fields are tagged `datapolicy:"secret"` and redacted by
`internal/secrets/` when `gcx config view` runs. See
[config-system.md](config-system.md) for the redaction implementation.

---

## RefreshTransport

`RefreshTransport` in `internal/auth/transport.go` is an `http.RoundTripper`
wrapper that intercepts outbound requests, detects access-token expiry,
performs a refresh inline, and forwards the original request with the new
bearer. It plugs into k8s `client-go` via `rest.Config.WrapTransport`.

A notable design choice: when `RefreshTransport` is active,
`rest.Config.BearerToken` is left empty. Setting it would cause `client-go`
to add a second authorization layer that would conflict with the refresh
logic. The transport also skips its own auth if the incoming request already
carries an `Authorization` header, letting providers pass through BasicAuth
credentials for datasource queries.

Refresh itself is serialized in two layers. Within a single process, a
`sync.Cond` funnels concurrent goroutines through one in-flight refresh.
Across processes, a `TokenLocker` hook holds a file lock on the config file
for the duration of the refresh. The network POST to `/api/cli/v1/auth/refresh`
uses a context detached from the caller's request context so that a caller
cancellation cannot abandon a refresh that has already consumed and rotated
the server-side refresh token.

---

## OAuth proxy routing

Cloud OAuth traffic does not go directly to the Grafana stack. It is routed
through the assistant backend, reached at
`https://<assistant>/api/cli/v1/proxy/*`. `rest.Config.Host` is rewritten to
that proxy URL at REST-config build time (`NewNamespacedRESTConfig` in
`internal/config/rest.go`), while a separate field
`NamespacedRESTConfig.GrafanaURL` preserves the original stack URL for
user-facing deep links (dashboard browser links, for example).

The proxy exists because Cloud OAuth tokens are scoped to the assistant
application, not the stack directly. The proxy exchanges the bearer
credential for stack access on each request.

---

## Concurrent-invocation safety

Multiple parallel `gcx` invocations can race on refresh-token rotation: the
first to refresh invalidates the old refresh token, which would cause later
callers to fail with a 401 and be locked out. gcx handles this with
cross-process file locking and coordinated token reloading in
`WireTokenPersistence` (`internal/config/rest.go`). At refresh time, the
`RefreshTransport` acquires a file lock on the config file, reloads it to
see any rotation that happened between request start and refresh time, skips
the network call if the on-disk tokens are already fresh, otherwise performs
the refresh and persists the new tokens before releasing the lock.

```mermaid
sequenceDiagram
    participant A as Invocation A
    participant B as Invocation B
    participant Config as Config file lock

    A->>Config: Acquire lock
    B->>Config: Try acquire lock (blocks)
    A->>A: Reload config
    A->>A: Refresh token (network)
    A->>Config: Write rotated tokens, release lock
    B->>Config: Acquire lock
    B->>B: Reload config (adopts A's rotation)
    B->>B: Use tokens directly (no refresh needed)
    B->>Config: Release lock
```

---

## File pointers

```
internal/auth/
  flow.go             OAuth PKCE flow, callback server, exchange,
                      state/endpoint validation
  transport.go        RefreshTransport, StoredTokens, TokenRefresher,
                      TokenLocker, TokenReloader, DoRefresh

internal/config/
  types.go            APIToken (SA token), CloudConfig.Token (CAP token),
                      OAuth* fields, ProxyEndpoint
  rest.go             Bearer attachment, WrapTransport wiring,
                      NewNamespacedRESTConfig, WireTokenPersistence,
                      ResolveTokenPersistenceSource

internal/auth/adaptive/
  — out of scope for this document (used by signal providers for
     adaptive telemetry auth). Follow-up documentation tracked separately.
```

---

## See also

- [Login system](login-system.md) — how the login orchestrator uses this subsystem.
- [Configuration and context system](config-system.md) — where tokens are stored and how secrets are redacted.
- [Login reference (user-facing)](../reference/login.md) — user-facing auth-method walkthroughs.
- [ADR 001: Login + config consolidation](../adrs/login-consolidation/001-login-config-consolidation.md) — historical rationale, including OAuth-proxy decision.
