# Error Recovery Reference

Recovery patterns for CLI failures encountered during diagnostic workflows.

**Flags verified against**: `cmd/gcx/datasources/query/command.go`, `cmd/gcx/config/command.go`, `cmd/gcx/datasources/`
**Source commit**: HEAD of branch `t1-cli-flag-audit`

---

## Failure Mode 1: Authentication / Authorization Error (401/403)

### Error Message Pattern

```
Error: request failed: 401 Unauthorized
Error: request failed: 403 Forbidden
Error: failed to list resources: 403 Forbidden — user does not have permission
```

### Likely Cause

- The configured API token is expired, revoked, or missing.
- The token exists but belongs to a service account without sufficient permissions for the resource type or namespace.
- The active context points to a Grafana instance that requires a different token.

### Corrective Action

1. Inspect the current context and verify the server URL and credentials are correct:

   ```bash
   gcx config view
   ```

2. If multiple contexts exist, confirm you are using the right one:

   ```bash
   gcx config current-context
   gcx config use-context <context-name>
   ```

3. Re-check the active context after switching:

   ```bash
   gcx config view --minify
   ```

4. If the token is known to be expired, update it with `gcx config set`:

   ```bash
   gcx config set contexts.<context-name>.grafana.token <new-token>
   ```

5. Retry the failing command after the context is corrected. For example:

   ```bash
   gcx datasources list -o json
   ```

---

## Failure Mode 2: Datasource Not Found

### Error Message Pattern

```
Error: datasource not found: <uid>
Error: no datasource with UID "<uid>" exists in this Grafana instance
Error: failed to query datasource: 404 Not Found
```

### Likely Cause

- The datasource UID used in `-d <uid>` does not exist in the current Grafana context.
- The UID was copied from a different environment (e.g., a production UID used against a staging context).
- The datasource has been renamed, deleted, or is provisioned under a different UID.

### Corrective Action

1. List all available datasources to find the correct UID:

   ```bash
   gcx datasources list -o json
   ```

   The output includes `uid`, `name`, and `type` for each datasource.

2. Filter to a specific datasource type (e.g., Prometheus or Loki) to narrow the list:

   ```bash
   gcx datasources list -t prometheus -o json
   gcx datasources list -t loki -o json
   ```

3. Inspect the full details of a specific datasource:

   ```bash
   gcx datasources get <uid> -o json
   ```

4. Retry the query using the correct UID from the listing output:

   ```bash
   gcx metrics query <correct-uid> '<expr>' --from now-1h --to now --step 1m -o json
   ```

---

## Failure Mode 3: Query Returns Empty Result Set

### Error Message Pattern

No error is raised; output contains an empty results array:

```json
{"status":"success","data":{"resultType":"vector","result":[]}}
```

Or for a range query:

```json
{"status":"success","data":{"resultType":"matrix","result":[]}}
```

### Likely Cause

- The metric or log stream selector does not match any active time series.
- The time range (`--from` / `--to`) falls outside the retention period or before the service was instrumented.
- The label filters in the query are too restrictive (e.g., a `job` label value that does not exist).
- The datasource is healthy but the service being queried is down and emitting no data.

### Corrective Action

1. Verify that the metric exists and is being ingested:

   ```bash
   gcx metrics metadata -d <uid> -m <metric-name> -o json
   ```

2. Check available label names and values for a Prometheus datasource:

   ```bash
   gcx metrics labels -d <uid> -o json
   gcx metrics labels -d <uid> -l job -o json
   ```

3. For Loki: confirm label names and streams are present:

   ```bash
   gcx logs labels -d <uid> -o json
   gcx logs labels -d <uid> -l service_name -o json
   ```

4. Broaden the time range to confirm whether data exists at all:

   ```bash
   gcx metrics query <uid> '<metric>' --from now-24h --to now --step 5m -o json
   ```

5. Simplify the query to remove label filters and verify the base metric returns data:

   ```bash
   # Before: http_requests_total{job="api",code="500"}
   # After (simplified):
   gcx metrics query <uid> 'http_requests_total' --from now-1h --to now --step 1m -o json
   ```

---

## Failure Mode 4: Query Timeout or Server Error (5xx)

### Error Message Pattern

```
Error: request failed: 504 Gateway Timeout
Error: request failed: 500 Internal Server Error
Error: context deadline exceeded
Error: failed to execute query: upstream timeout
```

### Likely Cause

- The query is too expensive for the time range and step combination (too many data points returned).
- The Grafana instance or the upstream datasource (Prometheus, Loki) is under high load.
- The step interval is too small, causing the query engine to evaluate too many windows.
- A very broad label selector (`{}`) or high-cardinality metric causes excessive backend processing.

### Corrective Action

1. Reduce the time range to limit the number of data points:

   ```bash
   gcx metrics query <uid> '<expr>' --from now-30m --to now --step 1m -o json
   ```

2. Increase the step interval to reduce the resolution and query load:

   ```bash
   gcx metrics query <uid> '<expr>' --from now-1h --to now --step 5m -o json
   ```

3. Add label filters to reduce cardinality:

   ```bash
   # Before: rate(http_requests_total[5m])
   # After (scoped):
   gcx metrics query <uid> 'rate(http_requests_total{job="api"}[5m])' --from now-1h --to now --step 5m -o json
   ```

4. Check Prometheus scrape targets to confirm the datasource is healthy:

   ```bash
   gcx metrics query -d <uid> 'up' -o json
   ```

5. Verify the Grafana instance is reachable by running a lightweight command:

   ```bash
   gcx datasources list -o json
   ```

   If this also times out, the issue is connectivity or instance-level; check the context configuration:

   ```bash
   gcx config view --minify
   ```

---

## Failure Mode 5: Malformed PromQL or LogQL Syntax Error

### Error Message Pattern

```
Error: bad_data: 1:25: parse error: unexpected <EOF>
Error: bad_data: parse error at char 15: unexpected identifier
Error: query parse error: <details>
Error: bad_data: invalid parameter "query": <details>
```

### Likely Cause

- Unmatched braces, brackets, or parentheses in the expression.
- Missing or misplaced label selectors (e.g., `{job=api}` instead of `{job="api"}`).
- Invalid function name or incorrect function argument count.
- Loki LogQL passed to a Prometheus datasource, or PromQL passed to a Loki datasource.
- Rate or aggregation window not specified (e.g., `rate(metric)` instead of `rate(metric[5m])`).

### Corrective Action

1. Check that the expression is syntactically complete — all `{`, `[`, and `(` must be closed:

   ```bash
   # Broken: rate(http_requests_total{job="api"[5m])
   # Fixed:
   gcx metrics query <uid> 'rate(http_requests_total{job="api"}[5m])' --from now-1h --to now --step 1m -o json
   ```

2. Confirm the datasource type matches the query language. List datasources and check the `type` field:

   ```bash
   gcx datasources list -o json
   ```

   Use Prometheus datasource UIDs for PromQL expressions, and Loki datasource UIDs for LogQL expressions.

3. Verify label values use double quotes, not single quotes or no quotes:

   ```bash
   # Wrong: {job='api'}
   # Wrong: {job=api}
   # Correct:
   gcx logs query <uid> '{job="api"} |= "error"' --from now-1h --to now -o json
   ```

4. For rate and increase functions, always specify the range window:

   ```bash
   # Wrong: rate(http_requests_total{code="500"})
   # Correct:
   gcx metrics query <uid> 'rate(http_requests_total{code="500"}[5m])' --from now-1h --to now --step 1m -o json
   ```

5. Use the Prometheus labels command to confirm label names and valid values before building complex queries:

   ```bash
   gcx metrics labels -d <uid> -o json
   gcx metrics labels -d <uid> -l code -o json
   ```

---

## Quick Reference: Recovery Command Cheatsheet

| Failure | First Diagnostic Command |
|---------|--------------------------|
| 401/403 auth error | `gcx config view` |
| Wrong context | `gcx config use-context <name>` |
| Datasource UID unknown | `gcx datasources list -o json` |
| Empty results | `gcx metrics metadata -d <uid> -m <metric>` |
| Query timeout | Increase `--step`, reduce time range |
| PromQL parse error | Check braces, quotes, and range windows |
| Loki parse error | Check stream selector syntax and double-quoted labels |
