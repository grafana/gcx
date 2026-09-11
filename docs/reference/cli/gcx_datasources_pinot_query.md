## gcx datasources pinot query

Execute a PinotQL query against a StarTree Pinot datasource

### Synopsis

Execute a PinotQL query against a StarTree Pinot datasource.

EXPR is the SQL query to execute, passed as a positional argument or via --expr.
Datasource is resolved from -d flag or datasources.pinot in your context.
Server-side macros ($__timeFilter, $__timeGroup, etc.) are supported.

Table name (StarTree tableName field):
  - StarTree requires tableName to load schema and expand macros before it runs
    pinotQlCode (sent alongside your SQL in the query request).
  - By default, gcx derives tableName from the first real FROM in the SQL.
  - Derivation supports schema.table, double-quoted identifiers, and the inner
    table in FROM (subquery) … (for example SELECT … FROM (SELECT … FROM events)).
  - Ignored for derivation: FROM inside string literals, line or block comments,
    or EXTRACT/TRIM/SUBSTRING/OVERLAY calls (they are not table clauses).
  - When the SQL has no extractable table (for example SELECT 1), pass --table
    with a table the query actually uses; the command fails otherwise.
  - --table overrides any name that would be derived from the SQL.

Row limit (--limit):
  - Default 100 when --limit is omitted on this command and the expression does not have a LIMIT; generic gcx datasources
    query uses the same Pinot default when the datasource kind is pinot.
  - --limit 0 disables enforcement (SQL is sent unchanged) and prints no notice.
  - Requests above 1000 are capped to 1000 in the emitted LIMIT when the SQL
    can be rewritten. --limit never overwrites an existing LIMIT n that is
    already at or below 1000.
  - Only SELECT/WITH-shaped PinotQL may be rewritten; optional leading SET …;
    prefixes are ignored for this check.
  - Left unchanged (no append): UNION, OFFSET, LIMIT offset,count, OPTION(…),
    trailing line comments, LIMIT before a trailing comment, unclosed block
    comments; keywords only in string literals or comments do not trigger these.
  - A LIMIT inside a comment (LIMIT /* note */ n) still counts as an existing
    LIMIT. The number is read after comments are blanked but the SQL is not rewritten because it is not safe to modify.

  Stderr notices (one line, never the full SQL):

  When --limit N is set:
    | Query                         | Sent            | Notice |
    |-------------------------------|-----------------|--------|
    | no LIMIT, safe                | append LIMIT N  | Query adjusted: appended LIMIT N (--limit). Use --limit 0 to disable enforcement. |
    | no LIMIT, not safe            | unchanged       | You asked for --limit N, but a row limit was not appended (this query shape is not safe to modify). Add LIMIT in the SQL or use --limit 0. |
    | has LIMIT, n == N             | unchanged       | (none) |
    | has LIMIT, n != N, n <= 1000  | unchanged       | You requested --limit N but the query already has a LIMIT, so no change was applied. |
    | bare LIMIT n, n > 1000        | LIMIT 1000      | You requested --limit N but the existing LIMIT was above the maximum, so it was reduced to 1000. |
    | LIMIT /* … */ n, n > 1000     | unchanged       | The query has a LIMIT above 1000 that should be reduced to 1000, but the query is not safe to modify. |

  When --limit is omitted (default 100):
    | Query                         | Sent            | Notice |
    |-------------------------------|-----------------|--------|
    | no LIMIT, safe                | append LIMIT 100 | Query adjusted: appended default LIMIT 100. Use --limit 0 to disable enforcement. |
    | no LIMIT, not safe            | unchanged       | (none) |
    | has LIMIT, n <= 1000          | unchanged       | (none) |
    | bare LIMIT n, n > 1000        | LIMIT 1000      | Query adjusted: LIMIT reduced to 1000 (maximum). Use --limit 0 to disable enforcement. |
    | LIMIT /* … */ n, n > 1000     | unchanged       | The query has a LIMIT above 1000 that should be reduced to 1000, but the query is not safe to modify. |

Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.

```
gcx datasources pinot query [EXPR] [flags]
```

### Examples

```

  # Simple query
  gcx datasources pinot query -d UID 'SELECT count(*) FROM events'

  # With time range
  gcx datasources pinot query -d UID --since 7d \
    'SELECT count(*) FROM events WHERE $__timeFilter("timestamp")'

  # Output as JSON
  gcx datasources pinot query -d UID 'SELECT 1 FROM events' -o json

  # Print a Grafana Explore share link for the executed query
  gcx datasources pinot query -d UID 'SELECT 1 FROM events' --share-link

  # Disable limit enforcement
  gcx datasources pinot query -d UID 'SELECT * FROM events' --limit 0

  # SQL with no extractable table
  gcx datasources pinot query -d UID --table events 'SELECT 1'
```

### Options

```
  -d, --datasource string   Datasource UID (required unless datasources.pinot is configured)
      --expr string         Query expression (alternative to positional argument)
      --from string         Start time (RFC3339, Unix timestamp, or relative like 'now-1h')
  -h, --help                help for query
      --jq string           jq expression to apply to JSON output. Mutually exclusive with --json.
      --json string         Comma-separated list of fields to include in JSON output, or 'list' (or '?') to discover available fields
      --limit int           Max rows to return; requests above 1000 are capped (stderr notice when the query is adjusted). 0 disables enforcement (default 100)
      --open                Open the executed query in Grafana Explore
  -o, --output string       Output format. One of: agents, json, table, wide, yaml (default "table")
      --share-link          Print the Grafana Explore URL for the executed query to stderr
      --since string        Duration before --to, or now if omitted (e.g., 30m, 6h, 7d); mutually exclusive with --from
      --step string         Query step (e.g., '15s', '1m')
      --table string        StarTree table name when the SQL has no extractable FROM (required in that case)
      --to string           End time (RFC3339, Unix timestamp, or relative like 'now')
```

### Options inherited from parent commands

```
      --agent                       Enable agent mode (JSON output, no color). Auto-detected from CLAUDECODE, CLAUDE_CODE, CURSOR_AGENT, GITHUB_COPILOT, AMAZON_Q, OPENCODE, PI_CODING_AGENT, or GCX_AGENT_MODE env vars.
      --config string               Path to the configuration file to use
      --context string              Name of the context to use (overrides current-context in config)
      --insecure-log-http-payload   Log full HTTP request/response bodies including raw credentials, authorization tokens, cookies, and OAuth refresh tokens. Do not ship these logs.
      --no-color                    Disable color output
      --no-truncate                 Disable table column truncation (auto-enabled when stdout is piped)
  -v, --verbose count               Verbose mode. Multiple -v options increase the verbosity (maximum: 3).
```

### SEE ALSO

* [gcx datasources pinot](gcx_datasources_pinot.md)	 - Query StarTree Pinot datasources

