# Provider Commands Reference

Patterns for implementing adapter-backed CRUD commands and provider-only
operations. For the shared data-access path, read
[`TypedCRUD` in the resource model](../../../docs/architecture/resource-model.md#typedcrud-and-resourceidentity)
and `internal/providers/slo/definitions/adapter.go` and `commands.go`.
Use `internal/providers/irm/incidents_commands_impl.go` for incident-specific
operations; older commands there are not universal CRUD templates.

## Output Format Compliance

Follow [output.md](../../../docs/design/output.md) for command defaults,
resource representations, and mutation results:

- Human-mode `list` defaults to the area's narrow table (`table` or `text`);
  `get` may use a single-row table or YAML. Match the surrounding commands.
- Register `wide` only when it adds useful detail. Derive supported formats
  from each command's `--help`; there is no universal four-format set.
- Adapter-backed reads use the registered resource representation in JSON/YAML,
  including singleton or read-only adapters. Preserve documented shipped output
  exceptions. Provider-only views and query results use their domain response.
- Tables may extract fields from typed values, but changing the output format
  must not change which data is fetched.
- New gcx-owned mutation results use the shared result family in
  `internal/output/mutation.go`; a status message belongs on stderr and does
  not replace a structured result on stdout. Preserve existing result contracts.

## CRUD Access Path

For resources exposed through both a provider command and a registered adapter,
use the shared `TypedCRUD[T]` factory and its `List`, `Get`, `Create`, `Update`,
and `Delete` methods. The product client sits behind that factory. This is the
[constitutional rule](../../../CONSTITUTION.md#architecture-invariants), so
new CRUD commands must not copy a legacy direct-client implementation.

Keep command code limited to options, input decoding, calling the typed method,
and encoding the result. For create/update, decode file or stdin through the
existing manifest path and handle errors; do not duplicate parsing templates or
bypass `TypedCRUD` to call `client.Create`.

Provider-only commands without adapter registration use their approved product
clients directly. Domain operations such as an incident's `close` action can
also use the product client for behavior outside the shared CRUD interface.
Do not create an adapter merely to make a provider-only `list` or `get` possible.

## Ancillary Subcommands

Map gcx subcommands that don't fit CRUD to provider `Commands()`:

```go
func (p *Provider) Commands() []*cobra.Command {
    loader := &providers.ConfigLoader{}
    cmd := &cobra.Command{
        Use:     "{provider}",
        Short:   p.ShortDesc(),
        Aliases: []string{...},
    }
    loader.BindFlags(cmd.PersistentFlags())
    cmd.AddCommand(
        // CRUD redirects
        newListCommand(loader),
        newGetCommand(loader),
        newCreateCommand(loader),
        newCloseCommand(loader),
        // Ancillary
        newActivityCommand(loader),      // nested group for writes (activity add)
        newListActivityCommand(loader),  // parent-scoped list compound (list-activity <id>)
        newSeveritiesCommand(loader),
        newOpenCommand(loader),
    )
    return []*cobra.Command{cmd}
}
```

### Common ancillary patterns

**Activity/timeline** — parent-scoped list compound (activity items are addressed by the parent's ID, not their own), plus a nested group for writes:
```
{provider} list-activity <id> [--limit N]
{provider} activity add <id> --body "..."
```

**Reference data** — list-only:
```
{provider} severities list
{provider} roles list
```

**Browser open** — construct URL from `restCfg.Host`:
```go
url := fmt.Sprintf("%s/a/grafana-{plugin}-app/{resource}s/%s", host, id)
exec.CommandContext(ctx, "open", url).Start()
```

## HTTP Client Reference Section Template

Phase 2 plan.md MUST include this section, filled in per provider. Copy this
template and replace placeholders with concrete values from the legacy CLI
source and current provider contract.

### Endpoint Table

| Method | Path | Purpose | Notes |
|--------|------|---------|-------|
| GET | `/api/v1/{resource}` | List all resources | Pagination: `?page={n}&limit={n}` or cursor |
| GET | `/api/v1/{resource}/{id}` | Get single resource | Returns unwrapped object (not envelope) |
| POST | `/api/v1/{resource}` | Create resource | Request body = resource JSON |
| PUT | `/api/v1/{resource}/{id}` | Update resource | Full replace, not PATCH |
| DELETE | `/api/v1/{resource}/{id}` | Delete resource | Returns 204 on success |

**CRITICAL:** Copy exact paths from the legacy CLI source and verify the current
API contract. Do NOT guess paths — many APIs have non-obvious patterns (org-scoped paths, plugin proxy paths, gRPC-style
POST-only endpoints).

### Auth and Client Construction

Record the actual request destination, selected credential source, any token
exchange, extra headers (for example `X-Scope-OrgID`), and the chosen client
factory. Resolve credentials and endpoints through `providers.ConfigLoader`.

Use the [provider guide's destination-based HTTP client rules](../../../docs/reference/provider-guide.md#step-4b-http-client-construction).
That is the construction reference for this plan: Grafana-host requests use the
configured Grafana transport; direct product-host requests use the approved
`httputils` factory after the direct-provider trust checks. A bare
`http.Client` and field-presence credential selection are not migration
patterns.

Name the existing constructor and fields the builder should use. If a new
constructor is required, document its signature here with the selected factory
and auth flow rather than copying a generic Bearer-token template.

---

## API Endpoint Gotchas

**CRITICAL:** Always check the legacy CLI source and current provider client
for exact endpoint paths. Don't guess.

Known inconsistencies in IRM API:
- `SeveritiesService.GetOrgSeverities` (not `SeverityService.GetSeverities`)
- `ActivityService.QueryActivity` (not `ActivityService.QueryActivityItems`)
- Activity query wraps in `{"query": {...}}`, not flat `{...}`

These naming inconsistencies are common in gRPC-style APIs. The ONLY
reliable sources are the current API contract and verified client code.
