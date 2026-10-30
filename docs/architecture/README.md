# Architecture Documentation

See `ARCHITECTURE.md` at the repository root for the architecture overview and navigation.

The deep-dive docs in this directory cover individual domains:

- [architecture.md](architecture.md) — Layer boundaries and request flow
- [auth-system.md](auth-system.md) — Authentication methods (OAuth PKCE, service account tokens, Cloud Access Policy tokens)
- [cli-layer.md](cli-layer.md) — Command composition and Options pattern
- [client-api-layer.md](client-api-layer.md) — Dynamic client, auth
- [config-system.md](config-system.md) — Contexts, env vars, TLS
- [data-flows.md](data-flows.md) — Push/Pull/Serve/Delete pipelines
- [login-system.md](login-system.md) — `gcx login` orchestration, sentinel-retry flow, validation pipeline
- [patterns.md](patterns.md) — Recurring patterns catalog
- [project-structure.md](project-structure.md) — Module and build boundaries
- [resource-model.md](resource-model.md) — Resource, Selector, Filter, Discovery
