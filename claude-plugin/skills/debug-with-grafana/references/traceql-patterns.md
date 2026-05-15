# TraceQL Patterns

Workflow and query patterns for Tempo trace search using gcx.

## Commands

| Command | Purpose | Positional arg |
|---------|---------|----------------|
| `gcx traces query [TRACEQL]` | Search traces by TraceQL expression | TraceQL expression |
| `gcx traces get TRACE_ID` | Fetch a single trace by ID | Trace ID (required) |
| `gcx traces labels` | List label names or values | None |

All commands accept `-d <uid>` for the Tempo datasource UID. `search` is an
alias for `query`. There are no `--tag` or `--service` flags — use TraceQL
expressions instead.

## Workflow: discover → search → get

### 1. Discover available tags

Start by listing tags, then inspect values for the ones that scope the problem.

```bash
gcx traces labels -d <tempo-uid>
gcx traces labels -d <tempo-uid> -l resource.service.name
gcx traces labels -d <tempo-uid> -l span.http.status_code
```

> **Common mistake**: `-l service.name` will fail — Tempo parses the dot as an
> identifier boundary. Always fully qualify: `-l resource.service.name`.
> Use `--scope resource` to filter labels by scope.

### 2. Search for traces

Build a scoped TraceQL query using the tag values you discovered. Scope as
tightly as possible — start with `resource.service.name` and add filters.

```bash
# Find error traces for a service
gcx traces query -d <tempo-uid> \
  '{ resource.service.name = "<service>" && status = error }' \
  --from now-1h --to now

# Find slow traces
gcx traces query -d <tempo-uid> \
  '{ resource.service.name = "<service>" && duration > 1s }' \
  --from now-1h --to now

# Filter by span name
gcx traces query -d <tempo-uid> \
  '{ resource.service.name = "<service>" && name = "GET /api/users" }' \
  --from now-1h --to now

# Filter by HTTP status
gcx traces query -d <tempo-uid> \
  '{ resource.service.name = "<service>" && span.http.status_code >= 500 }' \
  --from now-1h --to now

# Filter by root span service name
gcx traces query -d <tempo-uid> \
  '{ trace:rootService = "<service>" }' \
  --from now-1h --to now
```

If a query fails, go back to `traces labels` and check `--help` instead of
guessing further.

### 3. Get a specific trace

Once you have a trace ID from search results or from a log `trace_id` field:

```bash
gcx traces get -d <tempo-uid> <trace-id>
gcx traces get -d <tempo-uid> <trace-id> --llm    # LLM-friendly format
gcx traces get -d <tempo-uid> <trace-id> -o json
```

The trace ID is a positional argument — do not use `--trace-id` (it doesn't
exist).

## Attribute scoping rules

Tempo requires scoped attribute names. Unscoped dotted names cause parse errors.

**Custom attributes** use dot syntax:
- `resource.service.name`, `resource.k8s.namespace.name`
- `span.http.status_code`, `span.http.route`, `span.db.system`

**Intrinsics** use unscoped shorthand or colon syntax:

| Intrinsic | Type | Notes |
|-----------|------|-------|
| `name` / `span:name` | string | span operation name |
| `duration` / `span:duration` | duration | span duration |
| `status` / `span:status` | enum | `error`, `ok`, or `unset` |
| `kind` / `span:kind` | enum | `server`, `client`, `producer`, `consumer`, `internal` |
| `trace:rootName` | string | name of the root span |
| `trace:rootService` | string | service name of the root span |
| `trace:duration` | duration | end-to-end trace duration |

```bash
# WRONG — unscoped custom attribute
gcx traces query -d <tempo-uid> '{ service.name = "api" }'

# CORRECT — resource-scoped
gcx traces query -d <tempo-uid> '{ resource.service.name = "api" }'

# WRONG — these identifiers don't exist
gcx traces query -d <tempo-uid> '{ rootServiceName = "api" }'

# CORRECT — trace-scoped intrinsic
gcx traces query -d <tempo-uid> '{ trace:rootService = "api" }'
```
