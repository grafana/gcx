# Raw SQL dialects

For a datasource whose `query` leaf takes raw SQL (postgres, mysql, athena,
clickhouse and any future dialect). Read this before writing the client's
request builder or `--limit` handling — both pieces are shared, and hand-rolling
either is what turns a mechanical PR into three review rounds.

## Request body

Build the unified-query body with
`querysql.BuildRawQueryBody(pluginID, datasourceUID, req)`. Do not write a
per-dialect body builder: the bodies differ only in plugin ID and are otherwise
identical, so a copy is dead weight the next dialect copies again.

The exception is a plugin that needs more than the shared body models, and there
are two ways to qualify: a non-string `format` (ClickHouse and Athena both send a
number), or extra request fields (Athena also carries `connectionArgs` for
region, catalog and database). Either one earns a dialect builder — check both
before assuming you can reuse. Say in the PR which case you are in, so a
reviewer does not have to diff the two to find out.

The resulting split inside `internal/query/sql` is: response parsing, table
formatting, LIMIT clamping and the raw-SQL request body shared; schema
discovery, the dialect's own `bail` rules, and any body the shared builder cannot
model local. The package doc on `types.go` states the same split — if the two
ever disagree, the code is right and the doc needs fixing.

## Limit enforcement

`--limit` is `querysql.EnforceLimit(sql, limit, maxLimit, bail)` with a
dialect-local `bail` predicate. Never a hand-rolled clamp, and never a silent
one:

- Reject a negative `--limit` in `Validate()`, before any query runs.
- Return the `capped` bool from the dialect wrapper and warn on stderr when an
  explicit `LIMIT` above `maxLimit` was lowered. Silently truncating a bound the
  caller wrote themselves is a completeness defect — see
  `.claude/skills/integrate-with-gcx/references/self-review.md` T3 for the
  disclosure rule and for why a stderr hint is sufficient here.

## The bail predicate

`bail` decides which statements never get a `LIMIT` suffix. It must return true
for every shape where appending one changes the statement's meaning instead of
bounding it. Each trap below has shipped as a review finding on a real PR:

| Trap | What breaks |
|------|-------------|
| Statement-start allow-list | Allow-list the SELECT-shaped starts rather than bailing on a keyword list. MySQL accepts `LIMIT` on single-table `UPDATE`/`DELETE`, so a leaky allow-list silently restricts a write; and a line-anchored bail entry misfires on formatted queries (`ORDER BY x\nDESC`) |
| Keyword-vs-function ambiguity | MySQL's `REPLACE` is both a write statement and its most common string function. RE2 has no lookahead, so tell them apart by position — a write's keyword is never followed by `(` |
| Trailing line comment | A suffix appended after `--`/`#` lands *inside* the comment and is dropped, leaving the query unbounded. Match the dialect's exact comment-start rule, including the degenerate bare `--`: a pattern that is too tight there turns an invalid query into an unbounded scan |
| Dialect offset syntax | `LIMIT a, b`, `OFFSET n`, `FOR UPDATE`, `INTO OUTFILE` — a second `LIMIT` is a syntax error or a semantic change |

Test each entry by asserting the SQL that comes out, not just that something
bailed. A test that only checks "unchanged" passes for the wrong reason when the
allow-list rejected the statement before `bail` ever ran.

## Keep it proportionate

The row cap is a convenience bound, not a security control — `grafanaquery`'s
response-size limiting is the real backstop. So a `bail` that fails open on row
count is acceptable where one that mangles a statement is not, and there is no
need to grow the regex until it parses SQL. Say which way each entry fails in the
comment beside it: "fails safe" is true for writes and false for reads.
