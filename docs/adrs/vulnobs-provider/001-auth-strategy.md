# Vulnerability Observability Provider: Auth Strategy

**Created**: 2026-05-15
**Status**: proposed
**Supersedes**: none

## Context

Vulnerability Observability is exposed to clients only through Grafana's
instance plugin proxy at `/api/plugin-proxy/grafana-vulnerabilityobs-app/`.
The plugin stamps the user's existing Grafana auth onto upstream requests;
there is no separate token for the vulnerability-obs backend.

Two existing provider patterns are in scope as references:

| Pattern | Used by | Notes |
|---------|---------|-------|
| Same Grafana token (plugin proxy / plugin resources) | `kg`, `dashboards`, `slo`, `aio11y`, `appo11y`, `faro` | Reuses `config.NamespacedRESTConfig`; no new config keys. |
| Separate URL + token (external service) | `synth`, `irm` (some endpoints) | Adds provider-specific config keys. |

The vulnerability-obs API has no equivalent of a direct backend URL exposed
to clients — every call must traverse the plugin proxy.

## Decision

Reuse the same Grafana token via `config.NamespacedRESTConfig`, identical
to the KG provider. The HTTP client is built with `rest.HTTPClientFor(&cfg.Config)`
and requests target `/api/plugin-proxy/grafana-vulnerabilityobs-app/api-proxy/graphql/query`
on the active context's Grafana host.

Explicitly rejected:

- **Separate vulnerability-obs token / URL.** No upstream endpoint is
  documented and the plugin proxy is the supported integration point.
  Adding config keys would create a degree of freedom that doesn't exist
  in the product.
- **Configurable plugin ID.** The plugin's slug is part of its public
  contract on the Grafana instance; treat it as a constant in the client.

## Consequences

- Provider works out of the box for any user with an authenticated `gcx`
  context that can access the vulnerability-obs plugin in the Grafana
  instance (e.g., the `ops` context that points at
  `https://ops.grafana-ops.net`).
- No new config keys; no surface area in `gcx config view` / redaction
  to maintain.
- If Grafana ever exposes a direct backend URL or a CLI-friendly access
  policy scope, this ADR will need to be revisited.
