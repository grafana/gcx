# Logging In

`gcx login` creates or re-authenticates a context — a named collection of a Grafana server URL, credentials, and related settings. It auto-detects whether the server is Grafana Cloud or on-premises and adjusts the prompt accordingly.

This page walks through the common login paths, the mental model behind them, and how to recover from the errors you are most likely to see. If you already know which path applies, jump to the decision tree below.

## Pick your scenario

1. **Setting up Grafana Cloud interactively** → [Grafana Cloud (interactive OAuth)](#grafana-cloud-interactive-oauth)
2. **Setting up on-premises Grafana** → [Service account token](#service-account-token)
3. **Setting up CI, an agent, or any non-interactive environment** → [Environment variables (CI / agents)](#environment-variables-ci-agents)
4. **Adding Grafana Cloud product API access to an existing context** → [Grafana Cloud product APIs](#grafana-cloud-product-apis)
5. **Re-authenticating or switching between contexts** → [Re-authenticating and switching contexts](#re-authenticating-and-switching-contexts)

## Procedures

### Grafana Cloud (interactive OAuth)

The recommended flow for day-to-day use on a Cloud stack. Opens a browser for OAuth, then saves the access token, refresh token, and proxy endpoint to the named context and makes it current.

```bash
gcx login my-stack --server https://my-stack.grafana.net
```

When you run this:

1. gcx detects that `my-stack.grafana.net` is a Cloud host and presents an authentication-method prompt.
2. OAuth is the first option. Pick it (or accept the default) and gcx opens your browser.
3. Complete the OAuth flow in the browser; gcx receives the tokens on a local callback and writes them to the `my-stack` context.
4. The prompt then asks whether to add a Cloud Access Policy token for Cloud product API access — see the [Grafana Cloud product APIs](#grafana-cloud-product-apis) section. Press Enter to skip.

If OAuth does not suit your setup (corporate SSO restrictions, no browser available, etc.), pick "Service account token" at the prompt instead.

### Service account token

Works for both Grafana Cloud and on-premises. Required for on-premises (OAuth is only available on Cloud). Also the recommended path for non-interactive use.

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

For on-premises instances, gcx defaults the organization ID to 1 if you do not specify one — the common case for single-tenant Grafana OSS. If you need a different org ID, set it with `gcx config set contexts.my-grafana.grafana.org-id N` after login.

### Grafana Cloud product APIs

Commands under `gcx synth`, `gcx k6`, `gcx fleet`, and `gcx setup instrumentation` require a [Cloud Access Policy token](https://grafana.com/docs/grafana-cloud/account-management/authentication-and-permissions/access-policies/) in addition to Grafana auth. (`gcx irm` and `gcx slo` go through Grafana's plugin-resources surface on the stack and need only the service-account token.)

**Provide it at login:**

```bash
gcx login my-stack \
  --server https://my-stack.grafana.net \
  --token glsa_your_sa_token \
  --cloud-token glc_your_cap_token \
  --yes
```

**Add it later to an existing context by re-running `gcx login`:**

```bash
gcx login --context my-stack
# Follow the prompts; paste the Cloud Access Policy token when asked, or
# press Enter to skip.
```

`gcx` derives the Cloud stack slug from `--server` when the hostname matches a standard `*.grafana.net` pattern. For custom domains (such as `*.cloud.example.grafana.com`), set it explicitly:

```bash
gcx config set contexts.my-stack.cloud.stack your-stack-slug
```

You do not need to set `cloud.api-url` for `grafana.com`; gcx defaults to `https://grafana.com`. Set it only when you need a non-default Grafana Cloud API endpoint.

### Environment variables (CI / agents)

For pipelines, agents, and other non-interactive environments, skip `gcx login` entirely and provide credentials via environment variables. gcx resolves them on every command invocation.

```bash
export GRAFANA_SERVER="https://your-instance.grafana.net"
export GRAFANA_TOKEN="glsa_your_sa_token"
export GRAFANA_CLOUD_TOKEN="glc_your_cap_token"

# Optional: only needed when gcx cannot derive the stack slug from
# GRAFANA_SERVER (custom domains).
export GRAFANA_CLOUD_STACK="your-stack-slug"

gcx resources get dashboards
```

Environment variables take precedence over the config file when both are set. If you prefer, you can still run `gcx login` once to persist credentials to a named context and drop the env vars — the config-file path works for agents too.

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

**Contexts.** A context is a named bundle of server URL, credentials, and related settings stored in your gcx config file. Commands run against the *current* context unless you pass `--context` to target another one. The model and on-disk format mirror `kubectl` kubeconfig. See [configuration and context system](../architecture/config-system.md) for the full layout.

**Cloud vs on-premises.** gcx detects whether `--server` points at Grafana Cloud or an on-premises instance. The hostname is matched against known Cloud suffixes first (no network call); loopback and RFC1918 addresses are classified as on-premises; anything else is probed with a short HTTP request. The classification drives which auth methods appear in the prompt.

**Three auth methods, three API surfaces.** OAuth (browser-based, Cloud only) and service account tokens both authenticate to the Grafana API — dashboards, folders, datasources, alerts, plugin-hosted products (IRM, SLO, KG, App O11y), and the K8s-compatible `/apis` endpoints. Cloud Access Policy tokens are a separate credential used for GCOM (stack management) and the Cloud product APIs that bypass the stack — Synthetic Monitoring, k6, Fleet Management, and `gcx setup instrumentation`. A Cloud context typically holds two tokens: one for Grafana, one for Cloud. An on-premises context holds only a service account token.

**Interactive, `--yes`, and env-var modes.** Interactive mode opens prompts for anything you did not pass as a flag. `--yes` disables optional prompts and makes `gcx login` fail loudly if a required field is missing — the mode to use in CI. Environment variables (`GRAFANA_SERVER`, `GRAFANA_TOKEN`, `GRAFANA_CLOUD_TOKEN`, `GRAFANA_CLOUD_STACK`) skip `gcx login` entirely and resolve on each command invocation.

**Credential storage.** Tokens persist to the gcx config file under the context. `gcx config view` redacts secret fields when it prints. Do not commit the config file to version control.

## Troubleshooting

Each entry pairs the error you see with what it means and how to fix it.

1. **`missing contexts.X.grafana.org-id or contexts.X.grafana.stack-id`**
    - *Means:* gcx cannot determine which organization (on-prem) or stack (Cloud) the context targets.
    - *Fix:* `gcx config set contexts.X.grafana.org-id 1` for on-prem, or `gcx config set contexts.X.grafana.stack-id N` for Cloud. Issue [#545](https://github.com/grafana/gcx/issues/545) tracks auto-healing this.

2. **`cloud stack is not configured: set cloud.stack in config or GRAFANA_CLOUD_STACK env var`**
    - *Means:* a Cloud product API command ran against a context without a resolvable stack slug.
    - *Fix:* `gcx config set contexts.X.cloud.stack your-stack-slug`, or export `GRAFANA_CLOUD_STACK` in the current shell. Issue [#545](https://github.com/grafana/gcx/issues/545) tracks auto-healing this.

3. **OAuth: browser did not open, or token refresh failed**
    - *Means:* gcx tried to open a browser for OAuth but the system command returned an error, or the OAuth refresh flow failed.
    - *Fix:* Re-run `gcx login` to trigger a fresh flow. If your environment has no browser, use a service account token instead. For corporate proxies, check that the OAuth callback URL is reachable.

4. **`grafana version X is not supported; gcx requires Grafana 12.0.0 or later`**
    - *Means:* gcx requires Grafana 12 or newer because it uses the Grafana K8s-compatible `/apis` surface introduced in 12.
    - *Fix:* Upgrade your Grafana instance, or use a different tool for older versions.

5. **GCOM 401 / Cloud Access Policy token rejected**
    - *Means:* the Cloud Access Policy token was rejected by GCOM or a Cloud product API.
    - *Fix:* Verify the token at [grafana.com → Access Policies](https://grafana.com/docs/grafana-cloud/account-management/authentication-and-permissions/access-policies/). Rotate if compromised. Provide the new token via `gcx login --context X --cloud-token glc_...`.

6. **Health check or `/apis` connectivity failures**
    - *Means:* gcx could not reach the server during the validation pipeline — typically a wrong URL, DNS/proxy issue, or TLS mismatch.
    - *Fix:* Verify the server URL is correct and reachable. Check any corporate proxies (`HTTPS_PROXY`) and TLS configuration (`--insecure-skip-verify` for development only).

7. **Flag vs env-var precedence confusion**
    - *Means:* both a CLI flag and an environment variable are set for the same field, and gcx behaves unexpectedly.
    - *Fix:* Flags take precedence over env vars, which take precedence over config-file values. Run `gcx config view` to inspect the resolved config and spot the conflict.

## See also

- [`gcx login` flag reference](cli/gcx_login.md) — exhaustive list of flags and options.
- [Login system architecture](../architecture/login-system.md) — how the login orchestrator works internally.
- [Authentication subsystem](../architecture/auth-system.md) — OAuth PKCE, token lifecycle, `RefreshTransport`.
- [Configuration and context system](../architecture/config-system.md) — how contexts are stored and merged.
- [ADR 001: Login + config consolidation](../adrs/login-consolidation/001-login-config-consolidation.md) — historical rationale.
