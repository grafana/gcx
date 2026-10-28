# Loki query patterns

Use `gcx datasources loki query-patterns` (also available as `gcx logs query-patterns`)
to detect recurring log patterns for a bare LogQL stream selector:

```bash
gcx datasources loki query-patterns -d UID '{job="app"}' --since 1h -o json
```

The endpoint requires Loki's patterns API with the pattern ingester and querier
enabled. Results reflect the patterns and samples retained by the backend,
which prunes low-volume patterns and caps the number returned; they are not a
complete inventory of logs in the requested window.

JSON and YAML return a `status` and a `data` array. Each pattern includes:

- `pattern`: pattern text, with `<_>` placeholders for variable parts.
- `level`: log level, when supplied by Loki.
- `samples`: raw `[timestamp, count]` tuples, with timestamps in Unix seconds.
- `totalCount`: sum of the counts in the returned samples, including zero when
  there are no valid counts. Sample entries with fewer than two values are ignored.

All output formats sort patterns by `totalCount` descending, then by pattern
text alphabetically. Table and wide output show the same total in the `SAMPLES`
column. The total describes the returned samples, not all logs in the window.
