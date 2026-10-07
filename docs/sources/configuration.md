---
aliases:
  - /docs/grafana-cloud/as-code/observability-as-code/grafana-cli/gcx/configuration/
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

- A configuration file stores named stacks, named Grafana Cloud credentials, and contexts that bind them. `gcx` can layer system, user, and repository files. Check the [configuration file reference documentation](#configuration-reference) for all options.
  - If you have a file from an older `gcx` version, refer to [Migrate your gcx configuration](../migrate-configuration/).
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

{{< admonition type="note" >}}
If your default role is `None`, or you're using a custom role that doesn't grant the default `plugins.app:access` action, you'll also need to explicitly grant access to `grafana-assistant-app`:

```json
{
  "action": "plugins.app:access",
  "scope": "plugins:id:grafana-assistant-app"
}
```
{{< /admonition >}}

The `grafana-assistant-app.tokens.gcx:access` permission only lets you create `gcx` tokens for your own user. It doesn't grant access to other users' tokens and it doesn't extend your existing Grafana permissions.

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
file separately. See [Keychain credential storage](../keychain/)
for storage rules and keychain error procedures.

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

Without `--context`, the check covers every configured context before
returning. It exits non-zero when the current context is invalid or any checked
context fails configuration, authentication setup, connectivity, or Grafana
version checks, so it is safe to use as a deployment gate.

To check only one context without validating unrelated entries, pass
`--context`:

```shell
gcx config check --context staging
```

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

Every supported environment variable is listed in our [reference documentation](../cli-reference/#environment-variables).

Since `gcx` connects to Grafana through the REST API, you must configure authentication credentials. At minimum, set the Grafana URL and organization ID:

```shell
GRAFANA_SERVER='http://localhost:3000' GRAFANA_ORG_ID='1' gcx config check
```

Depending on your authentication method, also set one of the following:

- If you use a [Grafana service account](https://grafana.com/docs/grafana/latest/administration/service-accounts/) (recommended), set a [token](../cli-reference/#grafana_token).
- If you use basic authentication, set a [username](../cli-reference/#grafana_user) and a [password](../cli-reference/#grafana_password).

After you configure authentication, you can start using `gcx`.

If you want to persist this configuration, [create a context](#define-contexts).

<!-- BEGIN GENERATED CONFIGURATION REFERENCE -->

## Configuration reference

This schema describes configuration fields and their types in gcx **v1.5.0**.

```yaml
# Config holds the information needed to connect to remote Grafana instances.
# Version is the config format version. Version 1 is the only declared
# version accepted by this release; unsupported versions are rejected before
# migration or credential access. The field is absent on legacy configs,
# which the loader migrates to the current format after a safety preflight.
version: int
# Stacks is a map of Grafana stack configurations (connection, providers,
# per-stack resource settings), indexed by name. Contexts reference stacks
# by name via Context.Stack.
stacks:
  ${string}:
    # StackConfig holds the connection and provider configuration for a single
    # Grafana stack. Contexts reference stacks by name via Context.Stack.
    # Slug is the Grafana Cloud stack slug (e.g. "mystack").
    # Optional: if not set, the slug may be derived from Grafana.Server.
    slug: string
    grafana:
      # Server is the address of the Grafana server (https://hostname:port/path).
      # Required.
      server: string
      # User to authenticate as with basic authentication.
      # Optional.
      user: string
      # Password to use when using with basic authentication.
      # Optional.
      password: string
      # APIToken is a service account token.
      # See https://grafana.com/docs/grafana/latest/administration/service-accounts/#add-a-token-to-a-service-account-in-grafana
      # Note: if defined, the API Token takes precedence over basic auth credentials.
      # Optional.
      token: string
      # ProxyEndpoint is the assistant backend URL used as a reverse proxy for
      # OAuth-authenticated requests. Set automatically by `gcx login`.
      # This may differ from Server when cloud routing directs CLI traffic through
      # a separate endpoint (e.g. the assistant app backend).
      proxy-endpoint: string
      # OAuthToken is the OAuth access token (gat_) obtained via `gcx login`.
      oauth-token: string
      # OAuthRefreshToken is the refresh token (gar_) for renewing OAuthToken.
      oauth-refresh-token: string
      # OAuthTokenExpiresAt is the OAuthToken expiration time in RFC3339 format.
      oauth-token-expires-at: string
      # OAuthRefreshExpiresAt is the OAuthRefreshToken expiration time in RFC3339 format.
      oauth-refresh-expires-at: string
      # AuthMethod selects "oauth", "token", "basic", or "mtls" when no complete
      # runtime credential override supersedes it. Empty is valid for legacy configs
      # and uses compatibility inference; consumers should use
      # Context.EffectiveGrafanaAuthMethod instead of inspecting fields.
      auth-method: string
      # OrgID specifies the organization targeted by this config.
      # Note: required when targeting an on-prem Grafana instance.
      # See StackID for Grafana Cloud instances.
      org-id: int
      # StackID specifies the Grafana Cloud stack targeted by this config.
      # Note: required when targeting a Grafana Cloud instance.
      # See OrgID for on-prem Grafana instances.
      stack-id: int
      # TLS contains TLS-related configuration settings.
      tls:
        # TLS contains settings to enable transport layer security.
        # InsecureSkipTLSVerify disables the validation of the server's SSL certificate.
        # Enabling this will make your HTTPS connections insecure.
        insecure-skip-verify: bool
        # ServerName is passed to the server for SNI and is used in the client to check server
        # certificates against. If ServerName is empty, the hostname used to contact the
        # server is used.
        server-name: string
        # CertFile is the path to a PEM-encoded client certificate file.
        # This enables mutual TLS (mTLS) authentication with the server.
        cert-file: string
        # KeyFile is the path to a PEM-encoded client certificate key file.
        key-file: string
        # CAFile is the path to a PEM-encoded CA certificate bundle file.
        # When set, this CA is used to verify the server's certificate.
        ca-file: string
        # CertData holds PEM-encoded bytes (typically read from a client certificate file).
        # Note: this value is base64-encoded in the config file and will be
        # automatically decoded.
        cert-data:
          - int
          - ...
        # KeyData holds PEM-encoded bytes (typically read from a client certificate key file).
        # Note: this value is base64-encoded in the config file and will be
        # automatically decoded.
        key-data:
          - int
          - ...
        # CAData holds PEM-encoded bytes (typically read from a root certificates bundle).
        # Note: this value is base64-encoded in the config file and will be
        # automatically decoded.
        ca-data:
          - int
          - ...
        # NextProtos is a list of supported application level protocols, in order of preference.
        # Used to populate tls.Config.NextProtos.
        # To indicate to the server http/1.1 is preferred over http/2, set to ["http/1.1", "h2"] (though the server is free to ignore that preference).
        # To use only http/1.1, set to ["http/1.1"].
        next-protos:
          - string
          - ...
      # PathfinderInstalled caches that the Pathfinder plugin was detected as
      # installed and enabled on this server during `gcx login`. Once true, later
      # logins skip the detection probe and the one-time guide hint. In practice
      # the plugin is not uninstalled, so the flag is sticky and never cleared
      # automatically. Set automatically by `gcx login`.
      pathfinder-installed: bool
    # Providers holds per-provider configuration, indexed by provider name.
    # Each provider has a map of string key-value pairs.
    # Secret fields are selectively redacted by providers.RedactSecrets using
    # each provider's ConfigKey metadata.
    providers:
      ${string}:
        ${string}:
          string
    # Resources holds per-stack settings for the `gcx resources` commands,
    # merged (union) with the global Config.Resources.
    resources:
      # ResourcesConfig holds settings for the `gcx resources` commands.
      # AssumeServerDryRun lists resources ("<resource>.<group>", e.g.
      # "alertrules.rules.alerting.grafana.app") the user asserts honor server-side dry-run on
      # this stack, added to the built-in allowlist so --dry-run sends them to the server.
      assume-server-dry-run:
        - string
        - ...
# Cloud is a map of named Grafana Cloud (GCOM) auth entries. Contexts
# reference entries by name via Context.Cloud.
cloud:
  ${string}:
    # CloudEntry holds Grafana Cloud (GCOM) platform credentials and environment
    # configuration. Entries are named and referenced by contexts via
    # Context.Cloud; several contexts typically share one entry.
    # Token is a Grafana Cloud access policy token used to authenticate
    # against GCOM.
    token: string
    # OAuthToken is a grafana.com OAuth access token obtained via
    # `gcx cloud login`. The grafana.com OAuth flow issues no refresh token;
    # on expiry the user re-runs `gcx cloud login`.
    oauth-token: string
    # OAuthTokenExpiresAt is the OAuthToken expiration time in RFC3339 format.
    oauth-token-expires-at: string
    # OAuthScopes is the scope set granted by the OAuth token endpoint. It may
    # differ from the requested set and is retained so re-auth/keep operations do
    # not discard capability metadata.
    oauth-scopes:
      - string
      - ...
    # OAuthUrl is the base URL for the OAuth login flow run by `gcx cloud
    # login`. It is used only during login. Credential-bearing entries are
    # materialized as a coherent OAuth/API pair: one explicit endpoint fills its
    # missing peer; with neither set, gcx derives one unique referenced-stack
    # Cloud environment or falls back to "https://grafana.com". Incompatible
    # referenced environments are rejected and require separate entries.
    oauth-url: string
    # APIUrl is the base URL for all Grafana Cloud API (GCOM) resource calls
    # (stacks, regions, access policies, etc.). Every client talking to GCOM uses
    # it. It is materialized together with OAuthUrl so authentication and later
    # API calls stay in the same Cloud environment.
    api-url: string
# Resources holds global settings for the `gcx resources` commands,
# applying to all stacks. Merged (union) with each stack's Resources.
resources:
  # ResourcesConfig holds settings for the `gcx resources` commands.
  # AssumeServerDryRun lists resources ("<resource>.<group>", e.g.
  # "alertrules.rules.alerting.grafana.app") the user asserts honor server-side dry-run on
  # this stack, added to the built-in allowlist so --dry-run sends them to the server.
  assume-server-dry-run:
    - string
    - ...
# Contexts is a map of context configurations, indexed by name.
contexts:
  ${string}:
    # Context binds a stack and (optionally) a cloud auth entry together with
    # per-context defaults such as datasource UIDs.
    # Stack names the entry in Config.Stacks this context targets.
    stack: string
    # Cloud names the entry in Config.Cloud providing GCOM auth for this
    # context. Optional: without it, cloud-dependent operations fail at
    # runtime with a hint, not at validation time.
    cloud: string
    # Datasources holds per-kind default datasource UIDs, indexed by
    # datasource kind (e.g. "prometheus", "loki").
    datasources:
      ${string}:
        string
# CurrentContext is the name of the context currently in use.
current-context: string
# Diagnostics holds optional local diagnostic settings. All features are off by default.
diagnostics:
  # DiagnosticsConfig controls optional local diagnostic features.
  # AgentInvocationLog enables logging of failed agent-mode invocations to disk.
  # Off by default. When enabled, errors from agent-driven gcx calls are written
  # to LogDir (JSONL format) for capability-gap analysis.
  agent-invocation-log: bool
  # LogDir overrides the output directory for agent invocation log files.
  # Default: $XDG_STATE_HOME/gcx/ (platform-specific).
  log-dir: string
  # Telemetry controls anonymous usage telemetry: "enabled", "disabled",
  # or "log" (prints to stderr). Enabled by default. Overridden by the
  # GCX_TELEMETRY environment variable.
  telemetry: string
# Credentials controls how gcx persists credentials. It contains policy
# only; credential values remain on their owning stack and cloud entries.
credentials:
  # CredentialsConfig controls credential persistence without owning secrets.
  # Keychain selects whether credentials use the OS credential store. Valid
  # values are "on" and "off". The default is "on".
  keychain: string

```
