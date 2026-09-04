# Raw SQL dialects

For a datasource whose `query` leaf takes raw SQL (postgres, mysql, athena,
clickhouse and any future dialect). Read this before writing the client's
request builder or `--limit` handling — both pieces are shared.

**Copy postgres or mysql.** Step 1's advice to copy the closest existing client
needs a caveat here: athena and clickhouse predate the rules below. Neither
validates `--limit`, and both drop the `capped` flag the shared helper returns
(`internal/query/athena/types.go`, `internal/query/clickhouse/types.go` both do
`out, _ :=`), so they truncate silently. Their custom body builders are
legitimate — see the exception below — but their limit handling is not the model.

## Request body

Build the query body with
`querysql.BuildRawQueryBody(pluginID, datasourceUID, req)`. Do not write a
per-dialect body builder: the bodies differ only in plugin ID and are otherwise
identical.

However, if a datasource needs a non-string `format` argument (ClickHouse and
Athena both send a number), or extra request fields (Athena also has
`connectionArgs`), use a custom query builder. If you do this, point it out in
the PR description.

That leaves `internal/query/sql` holding what the dialects share — response
parsing, table formatting, LIMIT clamping, and the request body — while each
dialect package keeps its schema discovery, its `bail` rules, and a custom body
if it needs one. The package doc on `types.go` says the same thing; if the two
ever disagree, believe the code.

## Limit enforcement

Enforce `--limit` with `querysql.EnforceLimit(sql, limit, maxLimit, bail)` and a
dialect-local `bail` predicate. Two things make that a contract rather than a
clamp, and `internal/datasources/postgres/query.go` models both:

- Reject a negative `--limit` in `Validate()`, before any query runs.
- Take the `capped` bool the helper returns and warn on stderr when it is true.
  The caller's own `LIMIT` was lowered to `maxLimit`, and they should hear about
  it from the command that did it.

Handing back fewer rows than asked for without saying so is a completeness
defect — T3 in `.claude/skills/integrate-with-gcx/references/self-review.md`.
A stderr warning discharges it here because `docs/design/output.md` §15 is
PROPOSED and opt-in, not because this shape earns lighter treatment:
`QueryResponse{columns, rows}` is envelope-shaped, and T3's table would ask an
envelope for `list_meta` in the payload if §15 were binding.

## The bail predicate

`bail` decides which statements never get a `LIMIT` suffix. It must return true
for every statement where appending one changes what the statement *means*
rather than bounding what it returns. Each row below has shipped as a review
finding on a real PR:

| Statement shape | Why a LIMIT suffix breaks it |
|---|---|
| A write or a metadata statement | MySQL accepts `LIMIT` on single-table `UPDATE`/`DELETE`, so a suffix restricts a write instead of a read. Allow-list the SELECT-shaped starts rather than listing keywords to avoid — a line-anchored keyword bail also misfires on formatted queries, where `DESC` can begin a line of its own |
| A keyword that is also a function | MySQL's `REPLACE` is both a write statement and its most common string function. RE2 has no lookahead, so separate them by position: a write's `REPLACE` is never followed by `(` |
| A statement ending in a line comment | The suffix lands inside the `--`/`#` comment and never reaches the server, leaving the query unbounded. Match the dialect's exact comment-start rule, including a bare `--` with nothing after it |
| A limit the dialect spells another way | `LIMIT a, b`, `OFFSET n`, `FOR UPDATE`, `INTO OUTFILE` — a second `LIMIT` is either a syntax error or a change in meaning |

Test each row by asserting the SQL that comes out, not just that the statement
came back untouched. "Unchanged" can pass for the wrong reason: if the
SELECT-shaped allow-list rejected the statement first, `bail` never ran, and the
test proves nothing about the case it is named after.

## Keep it proportionate

The row cap is a convenience bound, not a security control — `grafanaquery`'s
response-size limiting is the real backstop. Prefer a `bail` that fails open on
row count over one that mangles a statement. There is no need to grow the regex
until it parses SQL. Say which way each entry fails in the comment beside it:
"fails safe" is true for writes and false for reads.
