# Logging In

`gcx login` creates or re-authenticates a context. The context binds a named
stack entry containing the Grafana server and authentication and, optionally, a
named Cloud entry containing Grafana Cloud platform authentication. The command
auto-detects whether the server is Grafana Cloud or on-premises and adjusts the
prompt accordingly.

This page walks through the common login paths, the mental model behind them, and how to recover from the errors you are most likely to see. If you already know which path applies, jump to the decision tree below.

## Pick your scenario

1. **Setting up Grafana Cloud interactively** → [Grafana Cloud (interactive OAuth)](#grafana-cloud-interactive-oauth)
2. **Setting up on-premises Grafana** → [Service account token](#service-account-token)
3. **Setting up CI, an agent, or any non-interactive environment** → [Environment variables for CI and agents](#environment-variables-for-ci-and-agents)
4. **Adding Grafana Cloud product API access to an existing context** → [Grafana Cloud product APIs](#grafana-cloud-product-apis)
5. **Re-authenticating or switching between contexts** → [Re-authenticating and switching contexts](#re-authenticating-and-switching-contexts)

## Procedures

### Grafana Cloud (interactive OAuth)

The recommended flow for day-to-day use on a Cloud stack. It opens a browser
for OAuth, saves the access token, refresh token, and proxy endpoint to the
context's named stack entry, and makes the context current.

```bash
gcx login my-stack --server https://my-stack.grafana.net
```

When you run this:

1. gcx detects that `my-stack.grafana.net` is a Cloud host and presents an authentication-method prompt.
2. OAuth is the first option. Pick it (or accept the default) and gcx opens your browser.
3. Complete the OAuth flow in the browser; gcx receives the tokens on a local callback and writes them to the stack entry bound by `my-stack`.
4. The Cloud platform step can keep an existing CAP or unexpired Cloud OAuth credential, accept a new CAP, run the experimental direct Cloud OAuth flow, or skip Cloud functionality. See [Grafana Cloud product APIs](#grafana-cloud-product-apis).

**Required role.** The OAuth consent step is gated by the `grafana-assistant-app.tokens.gcx:access` permission, granted by the **gcx User** role that the grafana-assistant-app plugin registers (available since plugin version 2.0.26). Grafana assigns the role automatically to users with the basic role Viewer or higher. The permission only allows minting gcx tokens for the requesting user; proxied requests run with the user's own identity and RBAC permissions. If the consent page shows a `Permission Required` error, see the [troubleshooting entry](#troubleshooting) below.

If OAuth does not suit your setup (corporate SSO restrictions, no browser available, etc.), pick "Service account token" at the prompt instead.

The persisted `auth-method` is authoritative unless a non-blank
`GRAFANA_TOKEN` selects service-account-token authentication for the current
invocation. That runtime selector is never written back to the config. In all
other cases gcx sends only the persisted method's credential even if an older
field remains in a hand-edited config, and an explicit non-mTLS method does not
present a stale client certificate. `GRAFANA_PASSWORD` can rotate a Basic
context's password but does not switch another explicit method to Basic. Legacy
contexts without `auth-method` retain OAuth → service-account token → Basic →
mTLS/anonymous inference, but a partial or rejected higher-priority credential
fails before any request instead of falling through. Repair the selected method
with `gcx login`, or use `gcx config edit --config <path>` when the file cannot
be loaded normally.

### Service account token

Works for both Grafana Cloud and on-premises and is the recommended path for
non-interactive use. Browser OAuth is Cloud-only; on-premises stacks can also
use configured mTLS client certificates. Basic auth remains supported by
manually configured contexts, but unified login does not offer a basic-auth
prompt.

**Non-interactive (recommended for automation):**

```bash
gcx login my-grafana \
  --server https://your-instance.grafana.net \
  --token glsa_your_token \
  --yes
```

**Interactive (gcx prompts for the token):**

```bash
gcx login my-grafana --server https://your-instance.grafana.net
# At the prompt, pick "Service account token" and paste your token.
```

Use a [Grafana service account token](https://grafana.com/docs/grafana/latest/administration/service-accounts/) with **Editor** or **Admin** role.

For on-premises instances, gcx defaults the organization ID to 1 if you do not specify one — the common case for single-tenant Grafana OSS. If you need a different org ID, set it with `gcx config set stacks.<name>.grafana.org-id N` after login (on the context's stack entry).

### Grafana Cloud product APIs

Commands under `gcx sm`, `gcx k6`, `gcx irm`, `gcx slo`, `gcx fleet`, and
other Cloud product surfaces need a Grafana Cloud platform credential in
addition to Grafana instance auth. A
[Cloud Access Policy token](https://grafana.com/docs/grafana-cloud/account-management/authentication-and-permissions/access-policies/)
has the widest compatibility and is recommended for automation. Direct Cloud
OAuth is available interactively but remains experimental, and some Cloud
product clients do not yet accept it.

#### Where to create the token and which scopes to grant

Create the token in either place:

- **In your stack** (deep-link the `gcx login` prompt offers): `https://<your-stack>.grafana.net/a/grafana-auth-app` → Access Policies → Create access policy.
- **From grafana.com**: **Administration → Cloud Access Policies → Create access policy** (`https://grafana.com/orgs/<your-org>/access-policies`).

See [Create access policies](https://grafana.com/docs/grafana-cloud/security-and-account-management/authentication-and-permissions/access-policies/create-access-policies/) for the step-by-step flow.

Scope the access policy to what you manage. `stacks:read` is the required baseline — it resolves your stack and gates login/token validation. Then add the scopes for the products you use:

| Scope | Enables |
|-------|---------|
| `stacks:read` | **Required.** Stack discovery (resolves the stack slug for all Cloud commands; also covers `gcx k6` reads) |
| `metrics:write`, `logs:write`, `traces:write` | Synthetic Monitoring and k6 (`gcx sm`, `gcx k6`) — these write verbs are needed to mint the Synthetic Monitoring token |
| `fleet-management:read` (and `fleet-management:write` for changes) | Fleet Management (`gcx fleet`) |
| `stacks:write` | Creating or updating stacks |
| `set:alloy-data-write` | Instrumentation Hub setup (`gcx instrumentation`) |

The Cloud Access Policy token is for Grafana Cloud product APIs (GCOM stack management, Synthetic Monitoring, k6, Fleet, IRM, SLO). Signal queries (`gcx metrics`, `gcx logs`, `gcx traces`, `gcx profiles`) authenticate with your Grafana token (OAuth or service account), not this token. When in doubt, start narrow and widen the policy as commands report missing-scope errors — the token can be re-scoped without re-running `gcx login`.

The interactive `gcx login` prompt links to this guidance when it offers Cloud
authentication choices.

**Provide it at login:**

```bash
gcx login my-stack \
  --server https://my-stack.grafana.net \
  --token glsa_your_sa_token \
  --cloud-token glc_your_cap_token \
  --yes
```

**Add Cloud access later to an existing context by re-running `gcx login`:**

```bash
gcx login --context my-stack
# Follow the prompts to keep or replace the existing credential, run Cloud
# OAuth, paste a Cloud Access Policy token, or skip.
```

**Run direct Cloud OAuth:**

```bash
gcx cloud login --context my-stack
```

The OAuth result is stored separately from a CAP, together with its expiry,
granted scopes, and exact OAuth/API endpoint pair. Keeping an existing
credential preserves its kind and metadata. OAuth has no refresh token; after
expiry, run Cloud login again. Use a CAP when you need full command
compatibility.

`gcx cloud login` persists a CAP only when `--cloud-token` contains a nonblank
value; it does not capture an ambient `GRAFANA_CLOUD_TOKEN`. Without a nonblank
flag value it runs the OAuth flow.

`gcx` derives the Cloud stack slug from `--server` when the hostname matches a standard `*.grafana.net` pattern. For custom domains (such as `*.cloud.example.grafana.com`), set it explicitly on the stack entry:

```bash
gcx config set stacks.my-stack.slug your-stack-slug
```

(`gcx config set stacks.<name>.slug your-stack-slug` does the same on the stack entry.)

You do not need to set Cloud endpoints for `grafana.com`; gcx defaults to
`https://grafana.com`. For a custom environment, the OAuth origin and API
destination must remain coherent. Supplying only one endpoint to a login flow
uses it for both operations; `gcx cloud login` accepts both `--oauth-url` and
`--api-url` when a deployment deliberately uses a distinct pair. Editing a
named entry's endpoint invalidates its existing credential, so authenticate
again after the change.

### Environment variables for CI and agents

For pipelines, agents, and other non-interactive environments, you can avoid a
persistent login and provide credentials through environment variables. gcx
resolves them on every command invocation and applies them only to the selected
context's runtime view.

```bash
export GRAFANA_SERVER="https://your-instance.grafana.net"
export GRAFANA_TOKEN="glsa_your_sa_token"
export GRAFANA_CLOUD_TOKEN="glc_your_cap_token"

# Optional: only needed when gcx cannot derive the stack slug from
# GRAFANA_SERVER (custom domains).
export GRAFANA_CLOUD_STACK="your-stack-slug"

gcx resources get dashboards
```

Environment variables take precedence over config-file values and are not
persisted incidentally. If you invoke unified `gcx login`, explicit flags still
win and the relevant environment inputs are used for the selected login target.
Direct `gcx cloud login` intentionally ignores an ambient Cloud token; pass
`--cloud-token` to persist one. You can instead run login once to persist
credentials and then remove the env vars.

Blank or whitespace-only credential variables are treated as unset. They do
not erase a stored token or password and do not authorize moving a stored
credential to an overridden destination.

### Re-authenticating and switching contexts

**Refresh credentials on the current context:**

```bash
gcx login
```

**Refresh a specific context:**

```bash
gcx login --context my-stack
```

Re-authentication preserves user-set fields (`org-id`, `stack-id`, TLS settings, provider-specific tokens) and updates only auth-bearing fields. If you manually set `org-id: 42`, it stays at 42 after re-auth.

With multiple config layers, gcx writes login results without an extra flag
only when the existing entries and context bindings have one provable owner.
If owners differ or a higher layer could shadow a rebind, the command stops
before authentication and lists exact `--config` choices. Consolidate the
bindings or explicitly select the intended file.

For discovery-mode login, gcx also snapshots every participating config file
before authentication. If any file changes, appears, or disappears while OAuth
or connectivity validation is running, persistence stops and the fresh
credential is not written. An explicit `--config` selection pins only that
chosen file.

For a pure mTLS login, `GRAFANA_TLS_*` paths remain runtime-only and gcx warns
that they were not saved. Keep supplying them, or persist the reviewed paths
explicitly. An `auth-method: mtls` context without both a client certificate
and private key fails before its next network request.

Token and OAuth credentials are destination-bound. If a bearer login depends
on runtime-only `GRAFANA_TLS_*` or `GRAFANA_PROXY_ENDPOINT` settings, gcx stops
before presenting or saving the credential and prints exact `gcx config set`
commands for the selected config, stack, and context. Those commands can also
initialize a deliberately selected, not-yet-created `--config` file. Persist
the reviewed settings and retry, or unset overrides that were not intended.
If a context or stack map key contains a dot, the dot-path grammar cannot name
it safely; gcx instead reports the exact fields and directs you to the selected
file with `gcx config edit --config ...`. If an OAuth issuer selects a proxy
different from `GRAFANA_PROXY_ENDPOINT`, persisting the override cannot repair
the conflict, so gcx tells you to unset that environment variable and retry.

**Switch which context is current:**

```bash
gcx config use-context my-stack
```

**View all configured contexts:**

```bash
gcx config view
```

Secrets are redacted in the output. See [configuration reference](configuration/index.md) for the full config file layout.

## How login works (mental model)

A short vocabulary so the troubleshooting entries below make sense. For the internal design, see the [login system architecture](../architecture/login-system.md) and [authentication subsystem](../architecture/auth-system.md) docs.

**Contexts.** A context binds together a named *stack entry* (server URL, Grafana credentials, provider config) and, optionally, a named *cloud entry* (grafana.com credentials, shareable across contexts) in your gcx config file. `gcx login` creates a stack named after the context plus the binding; `gcx cloud login` creates or reuses a cloud entry and binds it. Commands run against the *current* context unless you pass `--context` to target another one. The model and on-disk format mirror `kubectl` kubeconfig. See [configuration and context system](../architecture/config-system.md) for the full layout.

**Cloud vs on-premises.** gcx detects whether `--server` points at Grafana Cloud or an on-premises instance. The hostname is matched against known Cloud suffixes first (no network call); loopback and RFC1918 addresses are classified as on-premises; anything else is probed with a short HTTP request. The classification drives which auth methods appear in the prompt.

**Two authentication tiers.** Grafana OAuth, service account tokens, basic auth,
and mTLS authenticate to an individual Grafana instance — dashboards, folders,
datasources, alerts, and the K8s-compatible `/apis` endpoints. A CAP or direct
Cloud OAuth credential authenticates separately to GCOM and supported Cloud
product APIs. A Cloud context therefore commonly binds one Grafana credential
and one Cloud-platform credential; an on-premises context needs only Grafana
authentication.

**Interactive, `--yes`, and env-var modes.** Interactive mode opens prompts for
anything you did not pass as a flag. `--yes` disables optional prompts and makes
`gcx login` fail loudly if a required field is missing — the mode to use in CI.
Environment variables can avoid persistent login and resolve on each command
invocation; when login is invoked, its explicit flags take precedence over
those inputs.

**Credential storage.** Grafana credentials persist under the context's named
stack entry; CAP and Cloud OAuth credentials occupy distinct fields on the
referenced Cloud entry. When the OS keychain is available, token-shaped secrets
move there and YAML contains a source-, owner-, field-, and destination-bound
sentinel instead. `gcx config view` redacts secret fields. Do not commit a
credential-bearing config file to version control.

If a known locked or unreachable keychain backend prevents storing a brand-new
credential, gcx may keep that new value in the mode-`0600` config file and warns
that it remains plaintext. It never silently downgrades a replacement,
deletion, missing or rejected keychain reference, oversized value, or unknown
backend failure to plaintext.

## Troubleshooting

Each entry pairs the error you see with what it means and how to fix it.

1. **`missing stacks.X.grafana.org-id or stacks.X.grafana.stack-id`**
    - *Means:* gcx cannot determine which organization (on-prem) or stack (Cloud) the context's stack targets.
    - *Fix:* `gcx config set stacks.X.grafana.org-id 1` for on-prem, or `gcx config set stacks.X.grafana.stack-id N` for Cloud. Issue [#545](https://github.com/grafana/gcx/issues/545) tracks auto-healing this.

2. **`cloud stack is not configured: set the stack's slug (gcx config set stacks.<name>.slug <slug>) or GRAFANA_CLOUD_STACK env var`**
    - *Means:* a Cloud product API command ran against a context without a resolvable stack slug.
    - *Fix:* `gcx config set stacks.<name>.slug your-stack-slug`, or export `GRAFANA_CLOUD_STACK` in the current shell. Issue [#545](https://github.com/grafana/gcx/issues/545) tracks auto-healing this.

3. **OAuth: browser did not open, or token refresh failed**
    - *Means:* gcx tried to open a browser for OAuth but the system command returned an error, or the OAuth refresh flow failed.
    - *Fix:* Re-run `gcx login` to trigger a fresh flow. If your environment has no browser, use a service account token instead. For corporate proxies, check that the OAuth callback URL is reachable.

4. **`Permission Required` on the OAuth consent page (`gcx User` role / `grafana-assistant-app.tokens.gcx:access`)**
    - *Means:* your Grafana user lacks the `grafana-assistant-app.tokens.gcx:access` permission that gates gcx token minting. The **gcx User** role granting it is normally auto-assigned to Viewer and above, but the instance may run a grafana-assistant-app version older than 2.0.26 (role does not exist yet) or have customized basic-role grants.
    - *Fix:* ask your Grafana administrator to assign the **gcx User** role, or a custom role including `grafana-assistant-app.tokens.gcx:access`. If the role is missing entirely, the grafana-assistant-app plugin needs updating. As a workaround, authenticate with a service account token instead.

5. **`grafana version X is not supported; gcx requires Grafana 12.0.0 or later`**
    - *Means:* gcx requires Grafana 12 or newer because it uses the Grafana K8s-compatible `/apis` surface introduced in 12.
    - *Fix:* Upgrade your Grafana instance, or use a different tool for older versions.

6. **GCOM 401 / Cloud Access Policy token rejected**
    - *Means:* the Cloud Access Policy token was rejected by GCOM or a Cloud product API.
    - *Fix:* Verify the token at [grafana.com → Access Policies](https://grafana.com/docs/grafana-cloud/account-management/authentication-and-permissions/access-policies/). Rotate if compromised. Provide the new token via `gcx login --context X --cloud-token glc_...`.

7. **Health check or `/apis` connectivity failures**
    - *Means:* gcx could not reach the server during the validation pipeline — typically a wrong URL, DNS/proxy issue, or TLS mismatch.
    - *Fix:* Verify the server URL is correct and reachable. Check any corporate proxies (`HTTPS_PROXY`) and TLS configuration. For a reviewed development-only config, set `stacks.<name>.grafana.tls.insecure-skip-verify`; there is no login flag that bypasses TLS verification.

8. **`gcx assistant` commands fail with a service account token**
    - *Means:* `gcx assistant` commands (prompt, investigations) require OAuth, which is only available when you log in via the browser-based OAuth flow. Service account tokens are not supported.
    - *Fix:* Re-run `gcx login` and choose the OAuth (browser) option. If your environment cannot open a browser, `gcx assistant` is not available — use the Grafana UI instead.

9. **Flag vs env-var precedence confusion**
    - *Means:* both a CLI flag and an environment variable are set for the same field, and gcx behaves unexpectedly.
    - *Fix:* Flags take precedence over env vars, which take precedence over config-file values. For credential fields, blank or whitespace-only inputs are treated as unset rather than as an override. Run `gcx config view` to inspect the resolved config and spot the conflict.

10. **Login refuses to replace an existing context's server**
    - *Means:* the selected context points at a different Grafana server. `--yes` does not authorize changing a credential destination, and gcx will not present the stored credential to the new server.
    - *Fix:* Verify the new URL, then pass `--allow-server-override` together with a fresh `--token`, a fresh `GRAFANA_TOKEN`, or a new OAuth flow. An interactive login can ask for the same explicit confirmation.

11. **`Configuration write target is ambiguous`**
    - *Means:* layered files do not prove one owner for every entry or context binding the login may update.
    - *Fix:* Review the paths listed by the error and rerun with the intended `--config <path>`, or keep the target stack, Cloud entry, and context bindings together in one source.

12. **A credential was `rejected before network use`**
    - *Means:* a keychain reference was missing/foreign, a destination changed, or an environment credential was paired with an auto-discovered repository destination. gcx withheld it instead of sending an empty or misrouted credential.
    - *Fix:* For an auto-discovered repository destination, review the file and rerun with its explicit `--config` path. Explicit selection does not make a missing, foreign, or destination-mismatched keychain sentinel valid; re-authenticate or replace/unset that field. Use the exact raw editor command named by the error, such as `gcx config edit user` or `gcx config edit --config "<path>"`; it remains available even when ordinary loading fails.

13. **`Cloud credential destination is ambiguous`**
    - *Means:* one credential-bearing Cloud entry has no explicit endpoint pair and is referenced by contexts in different Cloud environments. gcx will not guess which API destination may receive it.
    - *Fix:* Run the exact raw `gcx config edit ...` command from the error, split the Cloud entry into one entry per environment, and update each `contexts.<name>.cloud` binding. Ordinary config loading remains blocked until the ambiguity is removed.

14. **`Configuration changed during authentication`**
    - *Means:* the selected owner or the discovered config source set changed while OAuth or connectivity validation was in progress. The freshly authenticated credential was not written.
    - *Fix:* Review every changed file, then retry. If you intend to trust one document as authoritative, rerun with its explicit `--config <path>`.

## See also

- [`gcx login` flag reference](cli/gcx_login.md) — exhaustive list of flags and options.
- [Login system architecture](../architecture/login-system.md) — how the login orchestrator works internally.
- [Authentication subsystem](../architecture/auth-system.md) — OAuth PKCE, token lifecycle, `RefreshTransport`.
- [Configuration and context system](../architecture/config-system.md) — how contexts are stored and merged.
- [ADR 001: Login + config consolidation](../adrs/login-consolidation/001-login-config-consolidation.md) — historical rationale.
