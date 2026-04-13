# ADR-001: CloudConfig in Context and GCOM Stack Discovery

**Created**: 2026-03-21
**Status**: accepted
**Bead**: none
**Supersedes**: none

## Context

gcx's config system was designed for Grafana instance auth: a single `grafana.server` URL + bearer token per context. Cloud providers (Fleet Management, OnCall, k6) need a different auth model:

- They authenticate with a **Grafana Cloud access policy token**, not a Grafana instance token.
- Their service URLs are not static — they must be **discovered per stack** via the GCOM API (grafana.com).
- Each provider was solving this independently: Fleet had `LoadFleetConfig` with its own `FLEET_URL`/`FLEET_TOKEN` env vars; OnCall and k6 were expected to do the same.

This created three problems:
1. **No shared auth primitive** — every cloud provider would need its own config keys, env vars, and loader.
2. **No URL discovery** — users had to look up service URLs manually per-stack.
3. **Naming inconsistency** — `LoadRESTConfig` was the only loader, but its name implied it was for REST APIs generically, when it specifically loads Grafana instance config.

## Decision

### 1. Add `CloudConfig` to `Context`

Extend the `Context` struct with a `Cloud` field holding a Grafana Cloud access policy token, optional stack slug override, and optional GCOM API URL override:

```go
type CloudConfig struct {
    Token  string `env:"GRAFANA_CLOUD_TOKEN"   json:"token,omitempty"`
    Stack  string `env:"GRAFANA_CLOUD_STACK"   json:"stack,omitempty"`
    APIUrl string `env:"GRAFANA_CLOUD_API_URL" json:"api-url,omitempty"`
}

type Context struct {
    Grafana *GrafanaConfig `json:"grafana,omitempty"`
    Cloud   *CloudConfig   `json:"cloud,omitempty"`  // NEW
    // ...
}
```

The stack slug is auto-derived from `grafana.server` when not set explicitly (e.g., `mystack` from `https://mystack.grafana.net`).

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
- All cloud providers share a single auth primitive — one `cloud.token` config key, one set of env vars
- Service URL discovery is automatic via GCOM — no manual URL lookup per stack
- Fleet (and future providers) need zero custom config keys: just `cloud.token` + a stack slug
- Config set UX is clean: `gcx config set contexts.mystack.cloud.token <token>`
- Stack slug is derivable from `grafana.server` for users who already have Grafana config — no duplicate config needed

### Negative
- `LoadCloudConfig` makes a network call to GCOM on every invocation — adds ~100ms latency to cloud provider commands
- Stack slug derivation only works for `*.grafana.net` and `*.grafana-dev.net` domains; self-hosted users must set `cloud.stack` explicitly
- Renaming `LoadRESTConfig` → `LoadGrafanaConfig` requires updating ~41 call sites

### Neutral
- The `--config` flag and env var override patterns remain unchanged
- GCOM API URL defaults to `https://grafana.com`; overridable per-context for dev/staging environments
