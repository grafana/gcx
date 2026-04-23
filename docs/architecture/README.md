# Architecture Documentation

See `ARCHITECTURE.md` at the repository root for the full architecture overview, navigation guide, key patterns, ADR index, and worked examples.

The deep-dive docs in this directory cover individual domains:

- [architecture.md](architecture.md) — Full system architecture
- [auth-system.md](auth-system.md) — Authentication methods (OAuth PKCE, service account tokens, Cloud Access Policy tokens)
- [cli-layer.md](cli-layer.md) — Command tree, Options pattern
- [client-api-layer.md](client-api-layer.md) — Dynamic client, auth
- [config-system.md](config-system.md) — Contexts, env vars, TLS
- [data-flows.md](data-flows.md) — Push/Pull/Serve/Delete pipelines
- [login-system.md](login-system.md) — `gcx login` orchestration, sentinel-retry flow, validation pipeline
- [patterns.md](patterns.md) — Recurring patterns catalog
- [project-structure.md](project-structure.md) — Build system, CI/CD, dependencies
- [resource-model.md](resource-model.md) — Resource, Selector, Filter, Discovery
