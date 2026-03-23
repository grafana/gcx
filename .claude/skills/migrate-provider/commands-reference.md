# Provider Commands Reference

Patterns for implementing CRUD redirect commands and ancillary subcommands.
Reference: `internal/providers/incidents/commands.go` for working examples.

## Output Format Compliance

> Reference: `docs/reference/design-guide.md` Section 1.3 (`[ADOPT]`)

All provider commands must comply with the default format rules:

| Command type | Default format | Required codecs | K8s wrapping for json/yaml |
|-------------|---------------|-----------------|---------------------------|
| `list` | `text` | `table` + `wide` | Yes — wrap via `ToResource` |
| `get` | `text` | `table` (single-row) | Yes — wrap via `ToResource` |
| `create -f` | Status message | — | Return created resource if `-o` specified |
| `close` / operational | Status message | — | No |

**Key rules:**
- `list` and `get` **must** call `ioOpts.DefaultFormat("text")` — do NOT leave `json` as default
- `list` and `get` **must** register both `table` and `wide` codecs via `RegisterCustomCodec`
- `table` columns: key identifying fields (ID/UID, name/title, status)
- `wide` columns: everything in `table` + additional detail (timestamps, labels, counts)
- json/yaml output wraps through `ToResource` to produce K8s-style envelope (`apiVersion`, `kind`, `metadata`, `spec`)
- Operational/query commands (activity, severities, etc.) are **exceptions** — they may use different defaults if the data is not a standard resource

---

## CRUD Redirect Pattern

Thin wrappers calling the client directly — NOT full re-implementations.

| Command | What it does |
|---------|-------------|
| `{provider} list` | `client.List` → table codec for table/wide, `ToResource` for json/yaml |
| `{provider} get <id>` | `client.Get` → K8s envelope via `ToResource`, encode |
| `{provider} create -f <file>` | Read YAML/JSON from file/stdin → parse unstructured → `client.Create` |
| `{provider} close <id>` | Convenience: `client.UpdateStatus` with "resolved" (or equivalent) |

### Key patterns

- All use `cmdio.Options` for output formatting (`-o json/yaml/table/wide`)
- `list` calls client directly for table output (avoids unstructured
  round-trip). For json/yaml, convert through `ToResource` for K8s envelope.
- `create` accepts file/stdin only — no flag-based convenience (`--title` etc.)
- No deprecation warnings — these are canonical paths

### Table codec

Export the codec type so `_test` package can use it:

```go
// IncidentTableCodec renders incidents as a tabular table.
type IncidentTableCodec struct {
    Wide bool
}

func (c *IncidentTableCodec) Format() format.Format { ... }
func (c *IncidentTableCodec) Encode(w io.Writer, v any) error { ... }
func (c *IncidentTableCodec) Decode(_ io.Reader, _ any) error {
    return errors.New("table format does not support decoding")
}
```

Register in command setup:
```go
opts.IO.RegisterCustomCodec("table", &IncidentTableCodec{})
opts.IO.RegisterCustomCodec("wide", &IncidentTableCodec{Wide: true})
opts.IO.DefaultFormat("table")
```

### Create from file/stdin

```go
var reader io.Reader
if opts.File == "-" {
    reader = cmd.InOrStdin()
} else {
    f, err := os.Open(opts.File)
    // ...
    reader = f
}

var obj unstructured.Unstructured
yamlCodec := format.NewYAMLCodec()
if err := yamlCodec.Decode(reader, &obj); err != nil { ... }

res, _ := resources.FromUnstructured(&obj)
inc, _ := FromResource(res)
created, _ := client.Create(ctx, inc)
```

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
        newActivityCommand(loader),
        newSeveritiesCommand(loader),
        newOpenCommand(loader),
    )
    return []*cobra.Command{cmd}
}
```

### Common ancillary patterns

**Activity/timeline** — nested subcommand group:
```
{provider} activity list <id> [--limit N]
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

## API Endpoint Gotchas

**CRITICAL:** Always check gcx source for exact endpoint paths. Don't guess.

Known inconsistencies in IRM API:
- `SeveritiesService.GetOrgSeverities` (not `SeverityService.GetSeverities`)
- `ActivityService.QueryActivity` (not `ActivityService.QueryActivityItems`)
- Activity query wraps in `{"query": {...}}`, not flat `{...}`

These naming inconsistencies are common in gRPC-style APIs. The ONLY
reliable source is the gcx client code.
