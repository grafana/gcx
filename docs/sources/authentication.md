---
aliases:
  - /docs/grafana-cloud/as-code/observability-as-code/grafana-cli/gcx/authentication/
title: Authenticate gcx
labels:
  products:
    - cloud
    - enterprise
    - oss
weight: 4
---

# Authenticate `gcx`

`gcx` supports four ways to authenticate to a Grafana instance or stack:

- [OAuth - Grafana Cloud only](#browser-based-oauth-cloud-only-recommended): Browser-based sign-in with `gcx login`. Recommended for interactive use. The tokens are user-scoped: every request runs with your own identity and RBAC permissions, so you can't access anything through `gcx` that you can't already access in the Grafana UI. Refer to [Required role for OAuth sign-in](#required-role-for-oauth-sign-in) for the permission this flow needs.
- [Service account token](#service-account-token): Works for Grafana Cloud and on-premises instances, and is the recommended method for CI and other non-interactive environments. Refer to [Grafana service accounts](https://grafana.com/docs/grafana/latest/administration/service-accounts/) for how to create one.
- [Basic authentication](#basic-authentication-self-managed-grafana): Username and password. Use this only when service accounts aren't available.
- mTLS: A client certificate and key for instances behind an identity-aware proxy. Configure the `grafana.tls` fields or corresponding TLS [environment variables](#environment-variables).

## No Grafana Cloud account yet

If you don't have a Grafana account, run:

```bash
gcx signup
```

`gcx` opens the Grafana Cloud sign-up page. After you create the account, verify your email, create your first stack, and approve "Connect gcx", the browser returns to gcx, which saves the connection. 

If you already have an account but no stack URL at hand, run `gcx login` and leave the server empty, or `gcx login --cloud --oauth` from a script or agent. See [First-time Grafana Cloud login](/reference/login#first-time-grafana-cloud-login).

## Browser-based OAuth (Cloud-only, recommended)

Best for day-to-day use on Cloud stacks, you can use this interactive option:

```bash
gcx login my-stack --server https://my-stack.grafana.net
```

If OAuth doesn't suit your setup, pick "Service account token" at the prompt.

This flow:

1. Opens a browser for OAuth.
1. Saves the access token.
1. Saves the access token, refresh token, and proxy endpoint to the `my-stack` context's named stack entry, and makes the context current.
1. `gcx` stops before the browser flow when the current process cannot write to the OS credential store. 

Note that agent users must approve the same command outside the sandbox. Refer to [Keychain credential storage](/keychain).

### Required role for OAuth sign-in

To authorize a `gcx` CLI connection with OAuth, your Grafana user needs the `grafana-assistant-app.tokens.gcx:access` permission. The **gcx User** role, registered by the Grafana Assistant application, grants this permission and is assigned automatically to users with the basic role Viewer or higher.

If your default role is `None`, or you're using a custom role that doesn't grant the default `plugins.app:access` action, you'll also need to explicitly grant access to `grafana-assistant-app`:

```json
{
  "action": "plugins.app:access",
  "scope": "plugins:id:grafana-assistant-app"
}
```

The `grafana-assistant-app.tokens.gcx:access` permission only lets you create `gcx` tokens for your own user. It doesn't grant access to other users' tokens and it doesn't extend your existing Grafana permissions.

If `gcx login` fails with a `Permission Required` error naming the **gcx User** role, ask your Grafana administrator to assign you the **gcx User** role, or a custom role that includes the `grafana-assistant-app.tokens.gcx:access` permission. If the role doesn't exist on your instance, update the Grafana Assistant application to a version that includes it.

### Grafana Cloud product API 

Grafana Cloud platform APIs use a separate credential stored in a named Cloud entry in addition to the Grafana instance auth. 

For automation, log in with a [Cloud Access Policy token](https://grafana.com/docs/grafana-cloud/account-management/authentication-and-permissions/access-policies/), which has the widest command compatibility. A CAP supports many Cloud management operations, but cannot enumerate user organisation memberships:

```bash
gcx login my-stack --server https://my-stack.grafana.net --token glsa_xxx --cloud-token glc_xxx --yes
```

You can also add Cloud access later by re-running `gcx login` against the same context. The interactive Cloud step can keep the existing CAP or unexpired OAuth credential, accept a new CAP, run the experimental browser-based Cloud OAuth flow, or skip Cloud functionality.

### Direct OAuth on Cloud

Direct Cloud OAuth is available through `gcx cloud login` or the interactive Cloud step of `gcx login`, but remains experimental and is not yet accepted by every Cloud product command. OAuth entries retain token, expiry, granted scopes, and a coherent OAuth/API endpoint pair.

```bash
gcx cloud login --context my-stack
```
To list your Cloud organisation memberships (slugs and roles):

```bash
gcx cloud login --context my-stack
gcx cloud orgs list --context my-stack
```

Default Cloud logins include `profile` alongside stack-management scopes. Run again `login` for existing credentials. `--scope profile` replaces the scope set, so use the default login to retain stack access. 

If `GRAFANA_CLOUD_TOKEN` or `cloud.<entry>.token` is set, unset it when using browser OAuth for organisation listing: access-policy tokens take precedence over OAuth tokens.

The command uses the selected context's Cloud API endpoint, including dev and ops environments. Membership is not a guarantee of
stack-creation permission. This differs from `gcx api /api/orgs`, which targets organisations inside a Grafana instance.

### Specify your org 

To specify the organisation slug when creating a stack:

```bash
gcx cloud stacks create --org example-org --name my-stack --slug mystack --region us --dry-run
```
Dry runs show the creation request. Review the preview, then omit `--dry-run` to create the stack. Creation defaults to YAML with `name`, `orgSlug`, `slug`, `status` and `url`. Use `stacks get <slug>` for full details. 

`gcx` derives the Cloud stack slug from `--server` when possible. Set it explicitly only for custom domains where gcx cannot derive it:

```bash
gcx config set stacks.my-stack.slug your-stack-slug
```

Both `stacks create` and `stacks list` require `--org <slug>`.

### Custom environments

You do not need to set Cloud endpoints for `grafana.com` as `gcx` defaults to `https://grafana.com`. 

For a custom environment, authenticate and store a coherent OAuth/API destination pair: supplying one endpoint to a login command
uses it for both operations unless you explicitly supply both to `gcx cloud login`. Changing a named entry's `api-url` or `oauth-url` invalidates its old credential, so re-authenticate after the edit.

## Service account token

Service account tokens work for Cloud or self-managed Grafana, and are useful for CI automation:

```bash
gcx login my-grafana --server https://your-instance.grafana.net --token glsa_xxx --yes
```

Use a [Grafana service account token](https://grafana.com/docs/grafana/latest/administration/service-accounts/) with a role matching what the token needs to do: **Viewer** is enough for querying (metrics, logs, traces, profiles) and reading dashboards or folders; **Editor** covers pushing and editing dashboards and folders; managing datasource configuration needs **Admin**. 

On Grafana Cloud and Enterprise, RBAC custom roles can scope query access tighter (for example `datasources:read` plus `datasources:query` on specific datasources). On-premises stacks can also use basic authentication or configured mTLS client certificates.

## Basic authentication (self-managed Grafana)

If you're using self-managed Grafana, you can also use basic auth, which prompts for a password without echoing it: 

```bash
gcx login my-grafana --server https://grafana.example.com --basic-auth --user admin
```

For automation, supply `GRAFANA_PASSWORD` and add `--yes`. 

Refer to [Basic authentication](/docs/reference/login#basic-authentication) for credential storage, auth switching, and validation behaviour.

## Environment variables 

For CI/CD or if you're using an AI agent, you can authenticate using environment variables:

```bash
export GRAFANA_SERVER="https://your-instance.grafana.net"
export GRAFANA_TOKEN="your-service-account-token"
export GRAFANA_CLOUD_TOKEN="your-cloud-access-policy-token"

# Optional: only needed if gcx cannot derive the stack slug from GRAFANA_SERVER.
export GRAFANA_CLOUD_STACK="your-stack-slug"
```

Env vars resolve at every command invocation, so you can run `gcx` commands directly without a prior `gcx login`.

For safety, an auto-discovered repository `.gcx.yaml` cannot attach runtime
tokens, prompted login credentials, or external mTLS keypairs to destinations
the file supplies. If you intend that file to own credentials or direct
provider endpoints, authorize it explicitly with `--config .gcx.yaml` or
`GCX_CONFIG=.gcx.yaml`; a `--server` or endpoint flag alone is not sufficient.
Provider-specific runtime endpoints are accepted only when their matching
runtime credential is supplied in the same invocation; that pair does not
authorize an auto-discovered repository stack's TLS or proxy configuration.

## Verify your configuration

Run `gcx config check` to check your configuration.

Refer to the [login reference](/docs/reference/login.md) for the full guide, including re-authentication, environment-variable setup, and troubleshooting for common errors.