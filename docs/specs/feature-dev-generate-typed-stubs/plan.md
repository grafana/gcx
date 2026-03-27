---
type: feature-plan
title: "dev generate: Typed Dashboard and Alert Rule Stubs"
status: draft
spec: spec/feature-dev-generate-typed-stubs/spec.md
created: 2026-03-09
---

# Architecture and Design Decisions

## Pipeline Architecture

```
User invokes: gcx dev generate dashboards/my-svc.go [alerts/cpu.go] [--type alertrule]
                                      |
                                      v
                          +----------------------+
                          |  generateCmd (cobra)  |
                          |  - parse positional   |
                          |  - validate args >= 1 |
                          |  - read --type flag   |
                          +----------+-----------+
                                     |
                     +---------------+---------------+
                     v               v               v
              +-------------+ +-------------+ +-------------+
              | processArg  | | processArg  | | processArg  |  (independent per arg)
              | 1. dirName  | | 1. dirName  | | 1. dirName  |
              | 2. inferTyp | | 2. inferTyp | | 2. inferTyp |
              | 3. inferNam | | 3. inferNam | | 3. inferNam |
              | 4. template | | 4. template | | 4. template |
              | 5. write    | | 5. write    | | 5. write    |
              +-------------+ +-------------+ +-------------+
                     |               |               |
                     v               v               v
              +----------------------------------------------+
              |  Summary: Generated N files, M failed        |
              +----------------------------------------------+
```

**Existing infrastructure reused:**
- `embed.FS` + `text/template` (same as `import` and `scaffold`)
- `ensureDirectory()` from `scaffold.go`
- `xstrings.ToSnakeCase` / `xstrings.ToCamelCase` from `import.go`
- `cmdio.Success` / `cmdio.Error` for output
- Options pattern (`generateOpts` struct with `setup(flags)`)

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Single new file `generate.go` in `cmd/gcx/dev/` | Follows the 1-file-per-subcommand convention (`import.go`, `scaffold.go`). All generate logic (type inference, arg parsing, template execution) fits in one file. (FR-001, FR-015, FR-016) |
| Type inference via `typeFromDir` map lookup | A `map[string]string` keyed by lowercased directory name provides O(1) lookup and trivial extensibility. Supports both singular and plural: `dashboards`, `dashboard`, `alerts`, `alertrules`, `alertrule`. (FR-003) |
| `--type` flag applies to ALL positional args | Matches batch semantics: if the user passes `--type alertrule`, every arg is treated as alertrule. This avoids per-arg type syntax. (FR-005) |
| File-exists check via `os.Stat` before writing | Open with `O_CREATE|O_EXCL` would also work, but an explicit stat-then-error gives a friendlier error message suggesting deletion. (Negative Constraint: no overwrite) |
| Two separate templates: `dashboard.go.tmpl` and `alertrule.go.tmpl` | Dashboard uses `Manifest()` helper; alertrule uses manual `resource.NewManifestBuilder()`. Separate templates keep each readable and independently testable. (FR-008, FR-009) |
| Template embeds produce gofmt-valid output with no post-processing | Templates use literal Go code with correct indentation. A unit test validates with `go/parser.ParseFile` and `go/format.Source`. (FR-014) |
| Package name from `filepath.Base(filepath.Dir(outputPath))` | Matches `import.go` line 129 convention. Lowercased. (FR-008, FR-009) |
| Process args sequentially, accumulate success/fail counts | No concurrency needed for local file writes. Sequential processing simplifies error reporting per FR-012. |
| Embed directive updated in `command.go` | The existing `//go:embed` directive in `command.go` owns the `templatesFS` variable. Adding `templates/generate/*.tmpl` to that directive is the minimal change. (FR-017) |

## Compatibility

**Unchanged:**
- `dev import` — no changes to converter logic, import templates, or importOpts
- `dev scaffold` — no changes to scaffold logic, scaffold templates, or scaffoldOpts
- `ensureDirectory` — reused as-is from `scaffold.go`
- `templatesFS` — same embed.FS variable, just with expanded glob

**Newly available:**
- `gcx dev generate` subcommand
- `templates/generate/dashboard.go.tmpl`
- `templates/generate/alertrule.go.tmpl`
