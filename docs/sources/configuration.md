---
title: Configure gcx
labels:
  products:
    - cloud
    - enterprise
    - oss
weight: 3
---

# Configure `gcx`

You can configure `gcx` with a configuration file or using environment variables.

- A configuration file stores named stacks, named Grafana Cloud credentials, and contexts that bind them. `gcx` can layer system, user, and repository files. Check the [configuration file reference documentation](https://github.com/grafana/gcx/tree/main/docs/reference/configuration/index.md) for all options. If you have a file from an older `gcx` version, refer to [Migrate your gcx configuration](../migrate-configuration/).
- Environment variables override the selected context in memory, so they work best in CI environments and are never persisted implicitly. Refer to [Configure `gcx` with environment variables](#configure-gcx-with-environment-variables) for more information.

## Choose an authentication method

`gcx` supports four ways to authenticate to a Grafana instance:

- **OAuth** (Grafana Cloud only): Browser-based sign-in with `gcx login`. Recommended for interactive use. The tokens are user-scoped: every request runs with your own identity and RBAC permissions, so you can't access anything through `gcx` that you can't already access in the Grafana UI. Refer to [Required role for OAuth sign-in](#required-role-for-oauth-sign-in) for the permission this flow needs.
- **Service account token**: Works for Grafana Cloud and on-premises instances, and is the recommended method for CI and other non-interactive environments. Refer to [Grafana service accounts](https://grafana.com/docs/grafana/latest/administration/service-accounts/) for how to create one.
- **Basic authentication**: Username and password. Use this only when service accounts aren't available.
- **mTLS**: A client certificate and key for instances behind an identity-aware proxy. Configure the `grafana.tls` fields or corresponding TLS environment variables.

Grafana Cloud platform APIs use a separate credential stored in a named Cloud
entry. A Cloud Access Policy token has full command compatibility and is
recommended for automation. Direct Cloud OAuth is available through
`gcx cloud login` or the interactive Cloud step of `gcx login`, but remains
experimental and is not yet accepted by every Cloud product command. OAuth
entries retain expiry, granted scopes, and a coherent OAuth/API endpoint pair.

### Required role for OAuth sign-in

To authorize a `gcx` CLI connection with OAuth, your Grafana user needs the `grafana-assistant-app.tokens.gcx:access` permission. The **gcx User** role, registered by the Grafana Assistant application, grants this permission and is assigned automatically to users with the basic role Viewer or higher.

This permission only lets you create `gcx` tokens for your own user. It doesn't grant access to other users' tokens and it doesn't extend your existing Grafana permissions.

{{< admonition type="note" >}}
If `gcx login` fails with a `Permission Required` error naming the **gcx User** role, ask your Grafana administrator to assign you the **gcx User** role, or a custom role that includes the `grafana-assistant-app.tokens.gcx:access` permission. If the role doesn't exist on your instance, the Grafana Assistant application needs to be updated to a version that includes it.
{{< /admonition >}}

## Understand the `gcx` configuration file in use

Run `gcx config path` to display the configuration files currently in use.

`gcx` stores configuration in YAML. `--config <path>` or `GCX_CONFIG=<path>`
selects one explicit file and bypasses layering. Otherwise, gcx loads every
existing source in this order, with later sources taking precedence:

1. System: the platform system config directory (for example, `$XDG_CONFIG_DIRS/gcx/config.yaml`).
2. User: `$HOME/.config/gcx/config.yaml`, falling back to the platform user config directory such as `$XDG_CONFIG_HOME/gcx/config.yaml`.
3. Repository: `.gcx.yaml` in the current working directory.

Named `stacks` and `cloud` entries are atomic across sources: a higher-priority
same-named entry replaces the lower entry completely. This keeps a credential
and its server or Cloud endpoint in the same trust source. Context references
and datasource defaults may merge field-by-field.

Credentials in the OS credential store (Keychain on macOS, Credential Manager
on Windows, Secret Service on Linux) are tied to the canonical config file,
exact owner kind and name, exact secret field, and normalized destination. Copying a config
file does not make its stored credentials portable; authenticate the copied
file separately.

An automatically discovered repository `.gcx.yaml` cannot attach tokens,
passwords, or client-certificate files from your environment, login flags, or
prompts to destinations the file supplies. It also cannot implicitly combine a
Cloud credential with a direct provider endpoint or write derived provider
credentials and caches. A provider endpoint supplied at runtime is accepted
only with its matching runtime credential, and neither value authorizes TLS or
proxy settings from an auto-discovered repository stack. To trust the
repository config for those operations, select it explicitly:

```bash
gcx login --config .gcx.yaml
# or
GCX_CONFIG=.gcx.yaml gcx login
```

Credentials already owned by that exact file remain usable while their bound
destination is unchanged.

Literal edits to a named stack or Cloud entry affect every context that
references it. If an edit changes a credential destination - such as a Grafana
server, Synthetic Monitoring URL, or Cloud API/OAuth endpoint - gcx clears the
old credential in the same write. Supply a fresh credential before using the
new destination. Normalization-equivalent endpoint edits preserve it.

## Define contexts

`gcx` supports multiple contexts so you can switch between instances. A context references a named stack entry, which holds the Grafana connection details. By default, `gcx` uses the `default` context.

A stack entry holds one credential alongside its server. To use two identities against the same stack - for example a personal token and a CI token, or read-only and admin - define two stack entries and a context for each.

To configure the `default` context:

```shell
gcx config set stacks.default.grafana.server http://localhost:3000
gcx config set contexts.default.stack default

# Set org-id when using OSS/Enterprise - skip when targeting Grafana Cloud
gcx config set stacks.default.grafana.org-id 1

# Authenticate with a service account token
gcx config set stacks.default.grafana.token service-account-token

# Or alternatively, use basic authentication
gcx config set stacks.default.grafana.user admin
gcx config set stacks.default.grafana.password admin
```

To create another context, use the same pattern:

```shell
gcx config set stacks.staging.grafana.server https://staging.grafana.example
gcx config set stacks.staging.grafana.org-id 1
gcx config set contexts.staging.stack staging
```

Note that in these examples, `default` and `staging` are the context and stack names.

## Useful commands

Use these commands to check the configuration:

```shell
gcx config check
```

The check covers every configured context before returning. It exits non-zero
when the current context is invalid or any context fails configuration,
authentication setup, connectivity, or Grafana version checks, so it is safe to
use as a deployment gate.

List existing contexts:

```shell
gcx config list-contexts
```

Switch to a different context:

```shell
gcx config use-context staging
```

See the entire configuration:

```shell
gcx config view
```

## Configure `gcx` with environment variables 

Every supported environment variable is listed in our [reference documentation](https://github.com/grafana/gcx/tree/main/docs/reference/environment-variables/index.md). 

Since `gcx` connects to Grafana through the REST API, you must configure authentication credentials. At minimum, set the Grafana URL and organization ID:

```shell
GRAFANA_SERVER='http://localhost:3000' GRAFANA_ORG_ID='1' gcx config check
```

Depending on your authentication method, also set one of the following:

- If you use a [Grafana service account](https://grafana.com/docs/grafana/latest/administration/service-accounts/) (recommended), set a [token](https://github.com/grafana/gcx/tree/main/docs/reference/environment-variables/index.md#grafana_token).
- If you use basic authentication, set a [username](https://github.com/grafana/gcx/tree/main/docs/reference/environment-variables/index.md#grafana_user) and a [password](https://github.com/grafana/gcx/tree/main/docs/reference/environment-variables/index.md#grafana_password).

After you configure authentication, you can start using `gcx`.

If you want to persist this configuration, [create a context](#define-contexts).
