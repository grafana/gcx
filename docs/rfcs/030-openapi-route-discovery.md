# RFC 030: Discover, inspect and call API routes

**Status**: proposed

## Summary

Add `gcx resources routes list`, `get` and `call` to discover and invoke
Grafana App Platform operations from published OpenAPI documents. Users can
inspect an unfamiliar API and call it without product-specific gcx code.
Short route names make common calls readable; full path templates provide
an unambiguous identity. This develops issue #1469.

`list` defaults to all eligible routes at each exposed API group's preferred
version. Route commands share the API group visibility policy used by
`resources pull` and `resources push`. `call` supports every advertised HTTP
method on eligible routes and requires explicit method selection.
For a successful JSON response, `call -o json` returns the API response body
directly, preserving ordinary field selection and `--jq` expressions.

## Motivation

Resource discovery tells callers which resource kinds exist, but does not
describe custom routes, their inputs or their responses. Users and agents
currently need separate API documentation and an authenticated raw request.
The useful workflow is to find a route, inspect its contract, then execute
the same operation with validated parameters.

## Scope

Limit route commands to API groups exposed by the existing resources surface.
Groups excluded from `resources pull` and `resources push` stay excluded from
`routes list`, `get`, route-specific help and `call`. Neither `--group` nor a
full path template bypasses this policy. Routes outside API groups are outside
this proposal's scope.

Within eligible groups, include advertised collection, item, subresource and
non-resource routes, including non-namespaced routes. Keep operation indexing
separate from managed-resource descriptors while sharing group visibility
policy. Apply the existing group exclusions without requiring an exposed
resource kind: an otherwise eligible group containing only custom routes is
in scope. In partially exposed groups, retain the existing resource allowlist:
expose routes for those resource types and their subresources, keeping other
resource types and unclassified group-level routes excluded until explicitly
added to the shared policy. This is gcx visibility policy, not per-user RBAC.

Reuse configuration, authentication, HTTP infrastructure and output codecs.
Keep existing resource CRUD and `gcx api` behavior compatible. Resource CRUD
changes, adapter verbs, full request-body schema validation, automatic
pagination, binary downloads and streaming output remain separate work.

The actual App Platform publication of custom methods such as `QUERY` has
not been verified. Supporting that representation is an integration
requirement, not an established capability of the current schema reader.

## User experience

The examples use fictional APIs. Operation names, schemas and defaults come
from server metadata, not a built-in widgets implementation. The `QUERY`
examples describe the intended contract; end-to-end support remains subject
to the publication check in Unresolved questions.

```sh
gcx resources routes list --group widgets.grafana.app
gcx resources routes get widgets/history --method QUERY
gcx resources routes call widgets/history \
  --method QUERY --name my-widget --limit 20 --data @request.json
```

For this example, `request.json` contains a JSON body matching the inspected
operation. Its shape comes from `get`; the CLI does not invent body flags.

### Find and inspect

`list` includes eligible CRUD and custom routes at the advertised preferred versions.
`--group`, `--version` and `--method` narrow discovery. An explicit
`--all-versions` option includes other versions; combining it with
`--version` is a usage error.

Each operation has a group, version, method and exact path template. Human
output must make ambiguous short names distinguishable and give callers a
copyable route identity. Show `ROUTE`, `GROUP`,
`VERSION`, `METHOD` and `SUMMARY`; use the short name when unique within that
group/version/method, otherwise show the full path template in `ROUTE`.
`-o wide` always includes the full path. Keep selectable route values intact
rather than truncating them into unusable selectors.

```console
$ gcx resources routes list --group widgets.grafana.app --method QUERY
ROUTE            GROUP                VERSION  METHOD  SUMMARY
widgets/history  widgets.grafana.app  v1       QUERY   Search change history

$ gcx resources routes get widgets/history --method QUERY
QUERY /apis/widgets.grafana.app/v1/namespaces/{namespace}/widgets/{name}/history

FLAG         LOCATION  TYPE     REQUIRED  DEFAULT
--namespace  path      string   yes       stacks-12345 (current context)
--name       path      string   yes       —
--limit      query     integer  no        20 (server default; range 1–100)
--cursor     query     string   no        —
--data       body      JSON     yes       —
```

The inspection continues with request and response schemas, available
examples and a suggested call; the excerpt above illustrates parameter
presentation. Display absent summaries as absent rather than generating
descriptions of an operation's behavior.

`get` shows every method for the selected path unless `--method` is given.
It keeps path placeholders intact, explains each parameter's explicit and
convenience flag forms, and shows required inputs, types, enums, bounds,
documented defaults, request/response schemas and examples. Server examples
and generated examples are labelled separately. Structured output includes
the same information and complete available schemas.

### Resolve and call

`call` requires `--method/-X`, including when the path has only one operation.
It accepts the issue's short name syntax, such as `widgets/history`, or an
exact advertised full path template. `--group` and `--version` disambiguate
short names. Do not select a path by looking at supplied parameter values,
and do not require a unique `operationId`.

Keep the short syntax where it is unique, and print
full path templates as alternatives where it is not. For example, both a
collection GET and an item GET can collapse to `widgets`:

```console
$ gcx resources routes call widgets --group widgets.grafana.app --method GET
error: widgets matches more than one route

Choose the collection:
  gcx resources routes call '/apis/widgets.grafana.app/v1/namespaces/{namespace}/widgets' --method GET

Choose an item:
  gcx resources routes call '/apis/widgets.grafana.app/v1/namespaces/{namespace}/widgets/{name}' --method GET --name '<name>'
```

Include a shell-quoted suggested invocation for each alternative, retaining
the selected method. Passing `--name` must not silently select the second
route. Full paths select the exact version in the path; conflicting `--group`
or `--version` flags are usage errors. This preserves two selector forms; its cost is
long selectors for common collection/item collisions.

An explicitly selected excluded group produces an error explaining that the
group is not exposed through `gcx resources`. It must not appear callable
merely because OpenAPI advertises its paths.

After route selection and input validation, `call` executes directly without
a confirmation prompt, including mutations. Explicit method selection is
required; OpenAPI metadata and the method name are not used to infer whether
a custom action is destructive. Listing a route does not establish permission
to execute it; the server authorizes the request.

```sh
gcx resources routes call \
  '/apis/widgets.grafana.app/v1/namespaces/{namespace}/widgets/{name}/history' \
  --method GET --path-param name=my-widget --query-param limit=20
```

Fill a path's `{namespace}` from the selected context unless explicitly
overridden. Do not add a namespace to a non-namespaced path. A query
parameter named `namespace` uses `--query-param namespace=...`.

Keep request bodies under `--data/-d`, supporting inline data, `@file` and
`@-`. Reuse `--header/-H` syntax. Validate required parameters, supported
parameter types, enums and bounds before issuing the operation. Unknown
parameters are errors, with a suggested correction where available.

Reserve gcx's own flags, including inherited flags. Offer direct parameter
flags only when the name is usable and unambiguous. Always retain
`--path-param name=value` and `--query-param name=value` for exact names and
collisions. Conflicting values are errors.

Keep the first parameter implementation small: string, integer, number and
boolean path/query values; scalar path parameters use standard simple
serialization, and scalar query parameters use form serialization. Support
query arrays of scalar values with `style: form, explode: true`, using a
repeated flag for each element. Do not split values on commas. Boolean flags
take explicit `true` or `false` values. Reject repeated scalar assignments,
including repeats through the explicit parameter form, rather than choosing
which value wins.

Show schema defaults as server defaults, but do not send values the caller
omitted. Namespace resolution is the explicit exception described above.
Reject missing required parameters even when their schema has a default.
Validate primitive types, enums and declared numeric/string/array bounds;
full schema composition and request-body schema validation remain out of
scope. A supplied parameter with an unsupported encoding produces an error
before the operation executes; inspection still shows its original schema.

Pass `--data` bytes unchanged. Prefer `application/json` when it is advertised
and no Content-Type is supplied; otherwise require an explicit advertised
Content-Type via `--header`. Check JSON syntax for JSON media types, without
applying full body-schema validation. Do not generate multipart bodies,
form fields or uploads from flags. Required bodies must be supplied; `--data`
never implies a method.

```sh
gcx resources routes call widgets/search \
  --method QUERY --query-param method=automatic --data @request.json
```

Generic `routes call --help` works without discovery. Route-qualified help
shows the selected operation's flags. It must not read request-body stdin,
require values for operation parameters, or execute the operation.

### Results and incomplete discovery

Successful JSON responses are returned directly through the shared output
system. A script can use `--jq '.items'` without a gcx wrapper. Empty successful
responses become JSON `null`; text responses become JSON strings. Explicit
JSON output and the agent default always produce valid JSON. A malformed body
advertised as JSON is a response error, not a successful text response.
Only finite JSON and text responses are supported initially. `get` still
describes binary and streaming contracts and marks them unsupported for
`call`. Reject a known unsupported response mode before execution. Select an
advertised finite JSON representation where available, then finite text,
and send the corresponding Accept header; an explicit Accept header must
select a supported advertised representation. Recognise streaming media
types, including event streams and media types with `stream=watch`.
Ordinary OpenAPI parameters and free-text descriptions do not reliably
identify a switch that changes a finite response into a stream; do not claim
that every such request can be rejected before execution.

Empty-response operations, including HEAD and 204-only operations, do not
need a response content type. Wildcard or missing media-type metadata is
incomplete evidence rather than a promise of JSON: request finite JSON and
validate the actual response. An unexpected binary or streaming response,
or invalid advertised JSON, produces an error stating that the request was
sent and its HTTP status. Do not automatically retry the operation or imply
that a mutation was rolled back. HTTP failures retain their status and
server error details through the shared error handling.

Read responses with the shared
[50 MiB response-size limit](../../internal/httputils/read.go) and a 60-second
request deadline, matching the [HTTP helper default](../../internal/httputils/client.go)
and preserving cancellation. These bounds also handle streams whose
headers incorrectly claim a finite representation; reaching a bound is an
error, never a truncated successful result. Verify response framing and
deadline behavior with local finite, streaming and mislabelled fixtures.

Discovery results must report failed documents and whether the requested
search was complete. Human diagnostics alone are insufficient: an agent
must understand completeness from structured output and the exit code.
For `list`, mixed successful and failed document fetches produce partial
results and exit 4. Include the selected discovery scope, completeness
and failed document identities in structured output; human output shows the
same limitation with an actionable diagnostic. Complete discovery with zero
matches is distinct from failure to discover matches.

The list result is a versioned gcx-owned object with `type: gcx.routes`,
`schema_version: "1"`, `items` and `discovery`; `items` contains operations.
`discovery` contains the requested scope, `complete` and a `failures` array
of document identities and structured errors. `get` uses
`type: gcx.route_description`, the same schema version and discovery metadata,
and a `route` object containing identity, operations and schemas. If discovery
cannot establish its selected route, `route` is null and the result carries
the shared error representation. A mixed list result also carries that error
representation with exit code 4.
Emit this result once through the shared codecs and preserve the exit code
without printing a second error document.

For `get` and `call`, require complete discovery within the selected scope before
resolving a short name. Without `--group`, the scope includes all eligible
groups at the selected versions. `--group` narrows that scope; an unrelated
group's failure does not block the call. A full path pins group, version and
path and requires its authoritative document plus the metadata needed to
check eligibility. It does not require unrelated documents to succeed.
`--method` cannot exclude an unavailable document from the search because its
methods are unknown. If required discovery fails, do not execute the
operation or report the route as absent; return a discovery error. The call's
success exit code describes the executed operation, not unrelated discovery.

`get` must not emit a selected route description when another candidate may
be hidden by failed discovery. A full path can still resolve within its
pinned scope. Once identity is established, unavailable schema detail is
reported within that route's description, without implying a different route
could have been selected.

`--json` projects operation or route fields while retaining result type,
schema version, discovery metadata and errors. The existing generic list
projection does not preserve arbitrary envelope fields, so wire this contract
explicitly and test it. Explicit `--jq` remains a caller-controlled transform
of the whole result; it does not change the process exit code.

Malformed selectors, ambiguous matches, invalid parameters and unsupported
input/output modes exit 2 before execution. Unavailable routes or discovery
failures exit 1, authentication/authorization failures exit 3, mixed discovery
results exit 4, and cancellation exits 5, following the
[exit-code taxonomy](../design/exit-codes.md). A `get` with multiple possible
paths returns an ambiguity error, not a description of whichever path was
loaded first. Incomplete discovery must never establish a unique match.

## Technical design

### Operation discovery

Introduce an operation index separate from managed-resource descriptors,
using the [resource registry's](../../internal/resources/discovery/registry.go)
group visibility policy as the shared source of truth. Do not maintain a
second list of excluded groups in the route implementation. The registry also
filters non-namespaced resources and subresources; those resource-shape
filters must not discard eligible custom routes.

Derive group/version identity from API group discovery and match the
corresponding OpenAPI index entry. Ignore entries outside advertised API
groups and versions. In partially exposed groups, match the resource segment
immediately following the group/version prefix and optional
`namespaces/{namespace}` prefix against the existing allowlist. A coincidental
resource name later in a path does not establish eligibility. Paths that
cannot be classified stay excluded in partially exposed groups.

The current [OpenAPI schema fetcher](../../internal/resources/discovery/openapi.go)
fetches documents associated with resource descriptors and extracts resource
spec schemas. Derive the expected document set from eligible API groups and
requested versions, then locate each document in the OpenAPI index. A missing
entry is a discovery failure identified by group/version, not evidence that
the group contains no routes. Fetch matching operation documents with bounded
concurrency, using the existing default of 10. Retain eligible operations from
`paths` and resolve parameter
and schema references within its documented support boundary. Deliberately
excluded documents do not count as discovery failures. Reuse lower-level
fetching and caching where appropriate without carrying over silent failures
for documents needed by the selected scope.

Use API group discovery for preferred-version information. If a route is
absent from the preferred version, report the group's other advertised
versions and suggest `list --all-versions`; do not claim the route exists in
those versions until their documents have been inspected. Never silently
switch versions. A missing preferred version is a discovery failure for
default selection; explicit `--version` and `--all-versions` can still select
advertised versions. `get` without `--method` resolves path identity across
methods and then describes every operation on that path.

Derive a short name by removing the group/version prefix, the leading
namespace prefix where present, and whole path-placeholder segments, then
joining the remaining literal segments with `/`. An empty result has no
short name. Resolve against all candidates in the selected scope, filtered
by method when supplied. Distinct path templates remain distinct even when
their short names coincide. Treat `operationId` as descriptive metadata.

Merge path-level and operation-level parameters by `(location, name)`, with
operation-level entries taking precedence. Resolve local JSON Pointer
references for parameters, request bodies, responses and schemas. Preserve
references and definitions in structured inspection; human schema rendering
stops recursive expansion at a visited reference. External references remain
visible but are not fetched. A missing or unsupported definition required
to serialize supplied inputs or enforce required inputs blocks execution;
missing response field detail alone does not prevent decoding finite JSON.

Parse operation fields according to the advertised OpenAPI version, retaining
raw schemas and unsupported features for inspection. Do not infer an HTTP
method from an arbitrary extension key. The exact supported representation
of `QUERY` remains an integration gate below.

Cache identity must include the configured server and document identity.
The current OpenAPI cache keys only the relative document URL and supplies
neither server isolation nor a TTL. Fetch the group list and OpenAPI index
fresh for each route invocation. Scope document-cache reuse to the server,
selected context and effective authentication identity. Reuse documents
under an explicitly advertised content hash; fetch unhashed documents each
time. An identity change invalidates reuse, and credentials themselves must
never be persisted as cache keys. If the authentication layer cannot supply
a safe identity, disable persistent document reuse for that invocation.
Do not fall back to stale metadata after discovery fails. This keeps
freshness tied to the server's current index without adding a new cache
control flag. Continue to authorize every actual request on the server.

### Parsing and transport

Keep CLI wiring in `cmd/` and route indexing, resolution, parameter validation
and request construction in `internal/`, following the
[architecture boundaries](../../CONSTITUTION.md#architecture-invariants).

Route-specific flags require two-stage parsing. Resolve gcx configuration,
route identity and method before interpreting operation parameters. Preserve
root initialization and ordinary flag placement. Cobra processes flags and
help before pre-run hooks, so merely registering flags in a pre-run hook is
insufficient. A bounded parser experiment must establish the concrete
integration before implementation is considered ready.

Extract reusable mechanics from [gcx api](../../cmd/gcx/api/command.go) into
`internal/` where needed. Reuse data/header syntax and the configured Grafana
transport; do not reuse implicit POST inference or method validation.
`gcx api` currently validates against a fixed vocabulary that omits `QUERY`.
Route calls instead select an advertised operation. Telemetry must retain
its closed vocabulary and must not collect arbitrary discovered paths or
method names.

Construct requests against the selected Grafana server. Full-path selectors
must match eligible advertised path templates; they are not arbitrary URL
inputs, and OpenAPI `servers` metadata does not change the destination.
Encode substituted path values as individual path segments and query values
with URL encoding, preserving the selected route. Do not concatenate raw
parameter values into the request URL. Verify that transport reuse does not
introduce automatic operation replay after a request may have been applied.
The configured [REST transport](../../internal/config/rest.go) includes retry
middleware; operation sends need an explicit no-retry path through that
shared configuration, preserving authentication and logging. Discovery GETs
can retain ordinary retry behavior.

`list` and `get` are finite structured commands with dedicated human codecs.
`call` is also finite, encoding the response value through shared codecs:
JSON by default for humans and `agents` by default in agent mode, with
explicit JSON/YAML output available. It returns no gcx mutation receipt
because the generic client cannot infer resource identity or mutation
semantics; the response-body contract applies to every method. Record this
command-specific rule in the output guidance during implementation rather
than treating `gcx api`'s raw protocol as precedent for new finite commands.
The new `call` verb means “execute the selected advertised HTTP
operation”; it requires the explicit naming review described in
[command naming](../design/command-naming.md).

## Drawbacks

Usability depends on the server's OpenAPI accuracy and availability. Dynamic
flags make help specific to a server and version, and add parsing complexity.
Generic operation calls cannot provide the domain-specific summaries and
safety knowledge of dedicated product commands.

Common collection/item aliases can be ambiguous and require long full-path
selectors. A global short-name call can also be blocked by incomplete
discovery in another eligible group; `--group` gives a smaller reliable
selection scope. Unsupported parameter encodings, external references and
binary/streaming responses remain inspectable but can limit which operations
can be called in the first version.

## Rationale and alternatives

All eligible routes at preferred versions provide a complete default view of
the selected scope without listing every version of each operation. A
custom-only default would require reliable classification and could hide
useful endpoints. Sharing the existing group visibility policy preserves the
resources surface's deliberate exclusions; exposing the entire OpenAPI index
would introduce a different visibility policy under the same command area.

Supporting every advertised method makes newly published APIs usable for
both reads and writes. A GET/QUERY-only first version would leave mutation
workflows dependent on raw requests. Direct execution follows the deliberate
request model of `gcx api`. Prompting based on HTTP method would interrupt
read-only POST operations while still lacking domain knowledge about custom
actions.

Returning the API's JSON body preserves natural shell composition. A status,
headers and body envelope would make metadata uniform but add nesting to
every response.

## Prior art

`gcx api` supplies authenticated raw requests and familiar body/header syntax.
Resource selectors demonstrate group/version disambiguation, but their
`resource/name` syntax identifies an object, not a route. Their parser must
not interpret `widgets/history` for this feature.

## Unresolved questions

The product scope and behavior above are selected. These implementation
checks remain open; a proposal is not evidence that they have passed.

- Obtain public, representative App Platform OpenAPI fixtures showing custom
  routes and `QUERY`, including routes without resource kinds. Establish the
  published representation and what the parser must preserve.
- Exercise two-stage parsing with inherited flag placement, collisions,
  misspellings, `--`, route help and `@-`, without executing a remote operation.
- Exercise local-reference resolution and recursive schemas against fixtures,
  including unsupported constructs. Inspection must distinguish unavailable
  detail from an empty schema; execution must not silently misencode inputs.
- Verify content-hash behavior and effective-authentication cache identity.
  Disable document-cache reuse where these guarantees cannot be established.

## Validation

Implementation must cover the issue's input-error and ambiguity examples,
including a missing name, out-of-range limit and misspelled parameter, plus:

- An unfamiliar eligible group, a group with no resource kinds, excluded
  groups and partial-group restrictions through every selector form.
- Preferred and explicit versions, namespaced and non-namespaced routes,
  duplicate short names, missing index entries and per-document failures.
- The same discovery details in human and structured output, with correct
  completeness and exit status; field selection preserves that metadata,
  and each finite invocation emits one result. Incomplete short-name `get`
  never emits a selected route description.
- Inherited flag placement, offline generic help, network-backed route help,
  conflicting inputs, repeated arrays and request bodies from files/stdin.
- Direct JSON bodies, empty and text responses, HTTP errors, unsupported
  binary/streaming modes and an unexpected response after a mutation.
- Existing `gcx api` and resource behavior, including telemetry privacy,
  shared transport and builds affected by any extracted helper.

Validate the parser with a local fixture before wiring operation execution.
Fixture coverage does not replace the representative server verification
needed for the `QUERY` publication contract.

## Future possibilities

Full body validation and automatic pagination may build on operation
metadata later. Dedicated product commands remain useful where domain
semantics improve the workflow.

## Terminology

- **Route**: an advertised path template within an API group and version.
- **Operation**: one HTTP method on a route.
- **Short name**: a convenient route selector, resolved before parameter values.
- **Preferred version**: the version advertised by API group discovery.
