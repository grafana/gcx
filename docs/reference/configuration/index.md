# Configuration reference

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
  # or "log" (prints to stderr). Overridden by the GCX_TELEMETRY and
  # DO_NOT_TRACK environment variables.
  telemetry: string

```
