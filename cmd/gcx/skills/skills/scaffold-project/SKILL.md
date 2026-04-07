---
name: scaffold-project
description: >
  Use when the user wants to create a new Grafana resources-as-code project,
  start a new dashboards-as-code repo, scaffold a gcx project, or asks
  "how do I get started with gcx". Triggers on phrases like "new project",
  "scaffold", "bootstrap", "create project", "get started".
---

# Scaffold a gcx Project

Scaffold a new Go project for managing Grafana resources as code using
`gcx dev scaffold`.

## Prerequisites

Verify gcx is installed:

```bash
gcx version
```

If missing, see the `setup-gcx` skill.

## Scaffolding

### Interactive Mode (recommended for first-time users)

```bash
gcx dev scaffold
```

Prompts for:
- **Project name** — becomes the directory name (kebab-cased)
- **Go module path** — e.g. `github.com/myorg/my-dashboards`

### Non-Interactive Mode

```bash
gcx dev scaffold --project my-dashboards --go-module-path github.com/myorg/my-dashboards
```

## What Gets Generated

```
my-dashboards/
├── .github/workflows/deploy.yaml   # CI/CD workflow for gcx push
├── .gitignore
├── go.mod
├── main.go                         # Entrypoint — registers all resources
├── internal/
│   └── dashboards/
│       ├── all.go                  # Registry function returning all manifests
│       └── sample.go              # Example dashboard using foundation-sdk builders
└── README.md
```

## Next Steps After Scaffolding

1. `cd my-dashboards && go mod tidy`
2. Configure gcx (example context `dev`):
   `gcx config set contexts.dev.grafana.server <URL>` and
   `gcx config set contexts.dev.grafana.token <TOKEN>`, then
   `gcx config use-context dev`
3. Edit `internal/dashboards/sample.go` or generate new stubs with `gcx dev generate`
4. Push to Grafana: `gcx resources push`

## Common Issues

| Issue | Fix |
|-------|-----|
| `go mod tidy` fails | Ensure Go 1.24+ is installed |
| Project name has spaces | Names are auto-kebab-cased; spaces become hyphens |
| Want to add alert rules | Create `internal/alerts/` and use `gcx dev generate alerts/my-rule.go` |
