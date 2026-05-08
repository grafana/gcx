# Copilot Working Instructions — Dan's gcx Fork

These instructions govern how the AI assistant works with Dan on this gcx fork.
They supplement (not replace) the project's own AGENTS.md and CONSTITUTION.md.

## Act vs Ask

**Default: act freely on file operations. Ask before git operations.**

- **Just do it:** Reading files, searching code, editing existing files, creating new files, running tests.
- **Ask first:** Adding new dependencies to go.mod.
- **Always ask:** `git push`, `git push --force`, deleting files/branches, any destructive operation.
- **When requirements are ambiguous:** Ask a clarifying question rather than guessing. One focused question is better than three.
- **When you know the answer:** Don't ask. If the CONSTITUTION, DESIGN, or ARCHITECTURE docs clearly specify how something should be done, follow them and proceed.

## Git Workflow

- **Branching:** Create feature branches for new work. Use `feat/kg-diagnose` style naming (lowercase, hyphenated, prefixed with `feat/`, `fix/`, or `refactor/`).
- **Commits:** Commit after each completed logical unit of work. Ask Dan to confirm before running `git commit`. Show the proposed commit message and a brief summary of what changed.
- **Commit messages:** Follow the gcx convention in `.gitmessage` — Title / What / Why format. Keep the title line under 72 chars.
- **Never:** `git push` without explicit approval. Never amend published commits. Never force-push.

## Testing

- **Run the full test suite** (`go test ./...`) after each set of changes.
- Follow the pre-commit checklist in AGENTS.md: `gofmt -w`, `mise run lint`, then `go test ./...`.
- If tests fail, diagnose and fix before moving on. Don't accumulate broken tests.

## Project Context

This fork is for proposing a `gcx kg diagnose` feature. The design spec is at:
`/Users/dan/Desktop/code/grafana/gcx-entity-graph-diagnostics-proposal.md`

Key knowledge sources in this workspace:
- `asserts-adi/` — Recording rules (3po), entity definitions (yoda), relationship YAML, service config
- `asserts-app-plugin/` — Grafana plugin backend (kgdatasource), TypeScript frontend, bundled dashboards
- `gcx/` — The CLI itself. Our changes go in `internal/providers/kg/`

The existing kg provider already has: client.go, commands.go, types.go, provider.go, resource_adapter.go.
Our new code should follow the same patterns visible in those files.

## Style

- Follow gcx conventions exactly. Read existing kg provider code before writing new code.
- Don't over-engineer. The proposal has 4 phases — implement one phase at a time.
- Don't add comments explaining obvious things. Do add comments for non-obvious domain logic (e.g., why `asserts_env` not `deployment_environment`).
- When referencing Asserts/Entity Graph domain knowledge, cite the source file in asserts-adi or asserts-app-plugin.
