# LogQL Selectors with gcx logs series

The `series` command requires at least one `--match` (`-M`) stream selector.
It accepts label matchers only - `=`, `!=`, `=~`, `!~` (RE2 regex) inside
`{...}` - not full LogQL.

## Supported vs Not Supported

Supported (label selectors):

```bash
gcx logs series -d <uid> -M '{job="varlogs"}'
gcx logs series -d <uid> -M '{job="api", namespace="production"}'
gcx logs series -d <uid> -M '{job=~"app-.*"}'
gcx logs series -d <uid> -M '{namespace!~".*-test", job!="debug"}'
```

Not supported by `series` - run these as full LogQL queries with
`gcx logs query -d <uid> '<logql>'` instead:

- Line filters: `{job="varlogs"} |= "error"`
- Parser stages: `{job="varlogs"} | json`
- Metric queries: `rate({job="varlogs"}[5m])`

## OR Logic: Multiple -M Flags

Labels inside one selector are ANDed. For OR, pass multiple `-M` flags:

```bash
# Job A OR Job B
gcx logs series -d <uid> -M '{job="varlogs"}' -M '{job="systemlogs"}'

# Different namespaces
gcx logs series -d <uid> -M '{namespace="prod"}' -M '{namespace="staging"}'
```

## Only Indexed Labels Are Valid Inside {}

Loki also has *structured metadata* (per-line keys attached at ingest,
including Loki's auto-added `detected_level`) and *parsed labels* (from
`| json` / `| logfmt`). Those are NOT indexed and must be filtered after a
pipe: `{app="myapp"} | detected_level="error"`, not
`{detected_level="error"}` (which matches nothing).

`gcx logs labels` lists the indexed labels - every value it returns is safe
inside `{}`. A key you saw in query output is only `{}`-valid if it appeared
in the `stream` map (`-o json`) / `STREAM` column, not under
`structuredMetadata` / `parsed` / `DETAILS`.

## Shell Quoting

Always use single quotes around the selector to prevent shell interpretation:

```bash
# Correct - single quotes outside
gcx logs series -d <uid> -M '{name="value", cluster="prod"}'

# Wrong - shell interprets quotes
gcx logs series -d <uid> -M {name="value"}

# Wrong - double quotes outside cause parsing errors
gcx logs series -d <uid> -M "{name='value'}"
```

## Workflow Tips

1. **Check label values first** - see what exists before guessing:

   ```bash
   gcx logs labels -d <uid> --label job
   gcx logs series -d <uid> -M '{job="<value-from-above>"}'
   ```

2. **Use regex for exploration** when you don't know exact values:

   ```bash
   gcx logs series -d <uid> -M '{job=~"app-.*"}'
   ```

3. **Use JSON output for large results** and pipe to jq:

   ```bash
   gcx logs series -d <uid> -M '{namespace="prod"}' -o json | jq '.data[] | select(.job=="api")'
   ```
