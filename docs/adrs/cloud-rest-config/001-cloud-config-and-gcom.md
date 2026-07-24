# ADR-001: CloudConfig in Context and GCOM Stack Discovery

**Created**: 2026-03-21
**Status**: accepted
**Bead**: none
**Supersedes**: none

> **Proposed amendment:** [ADR-022](../config-v1/001-versioned-split-config-and-secret-trust.md)
> would move Grafana connection/provider data to named `stacks`, move GCOM auth
> to named `cloud` entries, and make contexts thin bindings. The implementation
> follows that model while ratification remains pending. The GCOM discovery and
> shared `LoadCloudConfig` decisions below remain accepted.

## Context

gcx's config system was designed for Grafana instance auth: a single `grafana.server` URL + bearer token per context. Cloud providers (Fleet Management, OnCall, k6) need a different auth model:

- They authenticate with a **Grafana Cloud access policy token**, not a Grafana instance token.
- Their service URLs are not static — they must be **discovered per stack** via the GCOM API (grafana.com).
- Each provider was solving this independently: Fleet had `LoadFleetConfig` with its own `FLEET_URL`/`FLEET_TOKEN` env vars; OnCall and k6 were expected to do the same.

This created three problems:
1. **No shared auth primitive** — every cloud provider would need its own config keys, env vars, and loader.
2. **No URL discovery** — users had to look up service URLs manually per-stack.
3. **Naming inconsistency** — `LoadRESTConfig` was the only loader, but its name implied it was for REST APIs generically, when it specifically loads Grafana instance config.

## Decision (with the proposed ADR-022 model)

### 1. Use named stack and Cloud entries

Contexts are thin bindings. They reference one named `StackConfig` for Grafana
connection/provider data and one optional named `CloudEntry` for GCOM auth and
environment endpoints:

```go
type StackConfig struct {
    Slug    string
    Grafana *GrafanaConfig
}

type CloudEntry struct {
    Token      string
    OAuthToken string
    OAuthUrl   string
    APIUrl     string
}

type Context struct {
    Stack string
    Cloud string
}
```

The stack slug is stored on the stack entry and can be derived from
`stacks.<name>.grafana.server` when absent. Cloud credentials and their OAuth/API
environment live together in the referenced Cloud entry.

### 2. Add GCOM client in `internal/cloud/`

A `GCOMClient` calls the GCOM `/api/instances/{slug}` endpoint to discover stack service URLs. The `StackInfo` response includes instance URLs for Fleet Management, Prometheus, Loki, Tempo, Alertmanager, etc. — all the endpoints cloud providers need.

This replaces hardcoded or per-provider URL resolution with a single discovery call.

### 3. Add `LoadCloudConfig` to `providers.ConfigLoader`

A new `LoadCloudConfig(ctx) (CloudRESTConfig, error)` method on `ConfigLoader` orchestrates the full flow:

```
Config resolution → Stack slug derivation → GCOM discovery → CloudRESTConfig
```

Where `CloudRESTConfig` packages the token + discovered `StackInfo` for use by any cloud provider:

```go
type CloudRESTConfig struct {
    Token     string
    Stack     cloud.StackInfo   // has AgentManagementInstanceURL, etc.
    Namespace string
}
```

### 4. Rename `LoadRESTConfig` → `LoadGrafanaConfig`

The existing loader is renamed to reflect that it loads **Grafana instance** config specifically (server URL + service account token), distinct from the new Cloud config loader.

This also renames the `RESTConfigLoader` interface in incidents to `GrafanaConfigLoader`.

### Fleet provider refactored as proof of concept

Fleet Management becomes the first provider to use `LoadCloudConfig`, removing its `LoadFleetConfig` method, fleet-specific env vars (`FLEET_URL`, `FLEET_INSTANCE_ID`, `FLEET_TOKEN`), and `FleetConfigLoader` interface. Fleet now discovers its URL from `StackInfo.AgentManagementInstanceURL`.

## Consequences

### Positive
- All cloud providers share a single auth primitive — one referenced
  `cloud.<name>` entry and one set of environment variables
- Service URL discovery is automatic via GCOM — no manual URL lookup per stack
- Fleet (and future providers) need zero custom credential keys: just a named
  Cloud entry plus `stacks.<name>.slug`
- Config set UX is explicit: `gcx config set cloud.grafana-com.token <token>`
- Stack slug is derivable from `stacks.<name>.grafana.server` for users who
  already have Grafana config — no duplicate config needed

### Negative
- `LoadCloudConfig` makes a network call to GCOM on every invocation — adds ~100ms latency to cloud provider commands
- Stack slug derivation only works for recognized Grafana Cloud domains;
  otherwise users set `stacks.<name>.slug` explicitly
- Renaming `LoadRESTConfig` → `LoadGrafanaConfig` requires updating ~41 call sites

### Neutral
- The `--config` flag and env var override patterns remain unchanged
- GCOM API URL defaults to `https://grafana.com`; each named Cloud entry can
  carry a coherent OAuth/API pair for development or operations environments
