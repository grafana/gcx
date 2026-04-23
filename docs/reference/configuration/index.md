# Configuration reference

```yaml
# Config holds the information needed to connect to remote Grafana instances.
# Contexts is a map of context configurations, indexed by name.
contexts: 
  ${string}:
    # Context holds the information required to connect to a remote Grafana instance.
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
      # AuthMethod is the authentication method stored by gcx login: "oauth", "token", or "basic".
      # Empty string is valid for legacy configs; readers should call InferredAuthMethod() in that case.
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
          
    cloud: 
      # CloudConfig holds Grafana Cloud platform credentials and configuration.
      # Token is a Grafana Cloud API token used to authenticate against GCOM.
      token: string
      # Stack is the Grafana Cloud stack slug (e.g. "mystack").
      # Optional: if not set, the slug may be derived from Grafana.Server.
      stack: string
      # APIUrl is the base URL of the Grafana Cloud API (GCOM).
      # Optional: defaults to "https://grafana.com".
      api-url: string
    # DefaultPrometheusDatasource is the UID of the default Prometheus datasource to use for queries.
    default-prometheus-datasource: string
    # DefaultLokiDatasource is the UID of the default Loki datasource to use for queries.
    default-loki-datasource: string
    # DefaultPyroscopeDatasource is the UID of the default Pyroscope datasource to use for queries.
    default-pyroscope-datasource: string
    # DefaultTempoDatasource is the UID of the default Tempo datasource to use for queries.
    default-tempo-datasource: string
    # Datasources holds per-kind default datasource UIDs, indexed by datasource kind (e.g. "prometheus", "loki").
    # Takes precedence over the legacy DefaultXxxDatasource fields when both are set.
    datasources: 
      ${string}:
        string
    # Providers holds per-provider configuration, indexed by provider name.
    # Each provider has a map of string key-value pairs.
    # Secret fields are selectively redacted by providers.RedactSecrets using
    # each provider's ConfigKey metadata.
    providers: 
      ${string}:
        ${string}:
          string
# CurrentContext is the name of the context currently in use.
current-context: string
```
