# Migrating Knowledge Graph Cypher queries

Use `gcx kg graph query` for new Cypher workflows. It preserves the query's
projected columns and rows, including scalars, aggregates, nodes, relationships,
and paths.

`gcx kg entities query` is deprecated but remains supported through v1.x. Its
endpoint, flags, defaults, and `entities`/`edges` response stay unchanged.
Deprecation warnings go to stderr, so stdout remains usable by existing JSON
parsers. Removal will not happen before the next major release; this change
does not set a removal date or retire the backend search API.

## Choose the contract

| | Legacy command | New command |
|---|---|---|
| Invocation | `gcx kg entities query '<cypher>'` | `gcx kg graph query '<cypher>'` |
| API | `/v1/search/cypher` | `/v1/query/cypher` |
| JSON response | `entities`, `edges`, `pageNum`, `lastPage` | `columns`, `rows`, `stats` |
| Projection | Whole entities and edges | Ordered, positional values corresponding to `columns` |
| Result bounds | Existing `--page` behavior | Literal Cypher `LIMIT` required; optional `ORDER BY`/`SKIP` |
| Health | Existing `--insights-only` behavior | Fetch health with `kg entities list --insight any` or `kg entities inspect` |
| Default window | Last hour | Last hour |

These are separate contracts, not interchangeable aliases. There is no automatic
fallback from the new endpoint to the old one when a request fails.

## Migrate a caller

1. Add a literal `LIMIT` and a finite upper bound to any variable-length path.
   The server applies its language allowlist and query budgets; inspect the
   returned `CYPHER_*` code if it refuses a query.
2. Use `kg graph query` and update the output parser together. Remove `--page`
   and `--insights-only`; neither flag exists on the new command.
3. Compare results for the same explicit `--from`/`--to` window before switching
   a script or agent workflow. The legacy command remains available during this
   transition.

For example, a legacy caller retrieves whole entities and reads their names:

```bash
gcx kg entities query 'MATCH (s:Service) RETURN s LIMIT 10' --since 1h -o json \
  | jq '.entities[].name'
```

The new caller can project names directly:

```bash
gcx kg graph query 'MATCH (s:Service) RETURN s.name AS name LIMIT 10' --since 1h -o json \
  | jq '.rows[][0]'
```

A synthetic response to the new query is:

```json
{
  "columns": ["name"],
  "rows": [["checkout"], ["database"]],
  "stats": {"columnCount": 1, "rowCount": 2, "elapsedMs": 3}
}
```

Scalar projections avoid reconstructing entities just to read a property. When
returning whole nodes, read their `labels` and `properties`; the legacy entity's
`type`, `name`, `scope`, and `insights` fields are not the new node schema.
Relationship cells expose `type`, `startRef`, `endRef`, and `properties`.
Node and relationship refs are local to one response. Use identity properties
to correlate results across requests, and keep rows positional because column
names can repeat.

## Bounds and time windows

`stats.rowCount` describes returned rows, not the total number of matching
entities. `LIMIT` bounds the query result; there is no `lastPage` signal or
automatic pagination. Use literal `SKIP`/`LIMIT` and a deterministic `ORDER BY`
when selecting successive result windows, but do not treat separate requests
as a consistent snapshot of a changing graph.

The new command accepts a positive `--since` duration, or both `--from` and
`--to`. Explicitly empty time flags are rejected, and the start must precede the
end. Every matched node and relationship must overlap the requested window;
this does not require an entire path to coexist at one instant.

Keep `kg entities list` for basic entity lookup and `kg entities inspect` for
root-cause analysis with health evidence. Graph queries answer topology and
projection questions and do not return the legacy insight enrichment.
