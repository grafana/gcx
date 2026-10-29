# Help Text Standards

> Conventions for command descriptions (Use/Short/Long fields), examples format, and planned help topics for cross-cutting concerns.

---

## 8. Help Text Standards

### 8.1 Command Descriptions

| Field | Convention | Example |
|-------|-----------|---------|
| `Use` | `verb [RESOURCE_SELECTOR]...` | `list`, `get [SELECTOR]...` |
| `Short` | One sentence, period-terminated, no leading article | `List SLO definitions.` |
| `Long` | Expands on Short with usage context. 2-4 sentences. | `List all SLO definitions...` |

**Short** should start with a verb (imperative mood):

```go
// Good
Short: "List SLO definitions."
Short: "Push local resources to Grafana."

// Bad
Short: "A command that lists SLO definitions"
Short: "Lists SLOs"  // missing period
```

### 8.2 Examples Format

Examples are prefixed with a comment explaining intent. Show 3-5 examples per
command, progressing from simple to complex:

```go
Example: `  # List all SLOs (default: no row cap; API returns the full list)
  gcx slo definitions list

  # List SLOs with JSON output
  gcx slo definitions list -o json

  # Trim printed rows after fetch (optional; same API payload either way)
  gcx slo definitions list --limit 50

  # List SLOs from a specific context
  gcx slo definitions list --context=prod`,
```

### 8.3 Help Topics

Dedicated help pages for cross-cutting concerns:

| Topic | Content | Status |
|-------|---------|--------|
| `gcx help environment` | All env vars ([environment-variables.md](environment-variables.md)) | Planned |
| `gcx help formatting` | Output format guide, jq patterns | Planned |
| `gcx help exit-codes` | Exit code reference with descriptions and examples ([exit-codes.md](exit-codes.md)) | Implemented |

Implemented as a topic registry served by the help command, which delegates
ordinary command help to Cobra. Topics are listed as `gcx help <topic>` under
"Additional help topics:" alongside native Cobra topics, before the closing
`Use "gcx [command] --help"` line, and offered by `gcx help` completion.
They are not registered as standalone commands or included in `gcx commands`.
Tracked by R2.1, R2.2.
