# Vulnerability Observability Provider: GraphQL Client

**Created**: 2026-05-15
**Status**: proposed
**Supersedes**: none

## Context

The vulnerability-obs backend is a GraphQL service. All other gcx providers
target REST APIs and use the standard "hand-rolled HTTP client (~200 LOC)"
pattern documented in the provider guide. Three options for adapting:

1. **Bring in a GraphQL client library** (e.g., `github.com/Khan/genqlient`,
   `github.com/hasura/go-graphql-client`). Generated typed clients;
   compile-time guarantees on operations.
2. **Hand-roll a thin GraphQL POSTer.** Same shape as the REST clients in
   `kg/`, `slo/`, etc., but the body is `{operationName, query, variables}`
   instead of REST path + JSON.
3. **Use persisted-query hashes.** Match the UI's transport contract.

The product server **accepts unpersisted GraphQL documents** even though
the UI uses persisted queries (confirmed in research). Introspection is
disabled, so no codegen is possible without scraping the UI's `.graphql`
files (which we do not have access to without finding the plugin source).

## Decision

Hand-roll a thin GraphQL POSTer (option 2). The client exposes one private
method:

```go
func (c *Client) do(ctx context.Context, op, query string, vars any, out any) error
```

It marshals `{operationName, query, variables}` to JSON, POSTs to
`/api/plugin-proxy/grafana-vulnerabilityobs-app/api-proxy/graphql/query`,
checks for top-level `errors[]`, and unmarshals `data` into `out`.

Query documents are kept as Go string constants alongside the typed
response structs (one per query: `groups`, `sources`, `issues`).

Explicitly rejected:

- **Generated GraphQL client.** Adds a build-time dependency and codegen
  step for three small queries. The schema is undocumented and would
  drift silently. Not worth the complexity at v1 scope.
- **Persisted-query hashes.** Pinning hashes that the UI controls couples
  gcx releases to UI deployments; the unpersisted path avoids this
  coupling entirely.

## Consequences

- Provider client stays ~150 LOC and matches the structural shape of
  every other gcx provider client.
- Each new query is a string constant + a response struct + a one-line
  call. Adding `severityFilter`-aware `issues` or paginated `sources`
  later is mechanical.
- If the server ever stops accepting unpersisted queries (unlikely;
  Apollo Server's `forbidUnpersistedQueries` is opt-in and not currently
  enabled), gcx will break and the provider will need to switch to the
  persisted-query transport. Test coverage will catch this on the next
  smoke-test run.
- Type drift is detected at runtime, not compile time. Mitigation: every
  response struct has its own unit test that decodes a real captured
  payload.
