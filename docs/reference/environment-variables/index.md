# Environment variables reference

## `GCX_AUTO_APPROVE`

AutoApprove automatically enables the --force flag on delete operations,
enabling non-interactive operation in CI/CD pipelines.

## `GRAFANA_CLOUD_API_URL`

APIUrl is the base URL of the Grafana Cloud API (GCOM).
Optional: defaults to "https://grafana.com".

## `GRAFANA_CLOUD_STACK`

Stack is the Grafana Cloud stack slug (e.g. "mystack").
Optional: if not set, the slug may be derived from Grafana.Server.

## `GRAFANA_CLOUD_TOKEN`

Token is a Grafana Cloud API token used to authenticate against GCOM.

## `GRAFANA_ORG_ID`

OrgID specifies the organization targeted by this config.
Note: required when targeting an on-prem Grafana instance.
See StackID for Grafana Cloud instances.

## `GRAFANA_PASSWORD`

Password to use when using with basic authentication.
Optional.

## `GRAFANA_PROXY_ENDPOINT`

ProxyEndpoint is the assistant backend URL used as a reverse proxy for
OAuth-authenticated requests. Set automatically by `auth login`.
This may differ from Server when cloud routing directs CLI traffic through
a separate endpoint (e.g. the assistant app backend).

## `GRAFANA_SERVER`

Server is the address of the Grafana server (https://hostname:port/path).
Required.

## `GRAFANA_STACK_ID`

StackID specifies the Grafana Cloud stack targeted by this config.
Note: required when targeting a Grafana Cloud instance.
See OrgID for on-prem Grafana instances.

## `GRAFANA_TOKEN`

APIToken is a service account token.
See https://grafana.com/docs/grafana/latest/administration/service-accounts/#add-a-token-to-a-service-account-in-grafana
Note: if defined, the API Token takes precedence over basic auth credentials.
Optional.

## `GRAFANA_USER`

User to authenticate as with basic authentication.
Optional.
