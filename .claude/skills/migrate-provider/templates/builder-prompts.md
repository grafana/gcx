# Builder Spawn Prompt Templates

Templates for Phase 3 builder agents. Each builder receives spec.md (what),
plan.md (how), and their assigned implementation tasks — not verification tasks.

---

## Build-Core Spawn Prompt

```
You are Build-Core for the {provider} provider migration.

## Your Task

Implement the core adapter files for the {provider} provider.

**You own ONLY these files:**
- `internal/providers/{name}/types.go`
- `internal/providers/{name}/client.go`
- `internal/providers/{name}/client_test.go`
- `internal/providers/{name}/adapter.go`
- `internal/providers/{name}/resource_adapter.go`

Do NOT create or modify provider.go or any CLI command files. Those are owned by Build-Commands.

## CRITICAL: Response Envelope Shapes

Do NOT infer response envelope shapes. Copy deserialization code verbatim from
the grafana-cloud-cli source. If the source does `json.Unmarshal(body, &slice)`,
the new client MUST do the same — never wrap in a struct unless the source does.

## References

- **Spec**: {spec_path}/spec.md — functional requirements and acceptance criteria
- **Plan**: {spec_path}/plan.md — architecture decisions and HTTP client reference
- **Recipe**: `gcx-provider-recipe.md` Steps 2-5 (types, client, adapter, resource_adapter)
- **Conventions**: `conventions.md` — struct tags, linter traps, debug logging

## Implementation Tasks

{paste assigned task excerpts from tasks.md here}

## Completion

When all files are implemented and `make lint` passes on your files:
1. Mark the Build-Core task complete.
2. Send a message to the lead confirming completion and listing the files you created.
```

---

## Build-Commands Spawn Prompt

```
You are Build-Commands for the {provider} provider migration.

## Your Task

Implement the provider registration and CLI commands for the {provider} provider.
The core adapter (types, client, adapter, resource_adapter) has already been
implemented by Build-Core.

**You own ONLY these files:**
- `internal/providers/{name}/provider.go`
- `cmd/gcx/providers/{name}/commands.go`
- `cmd/gcx/providers/{name}/*_test.go` (command tests)
- The blank import line in `cmd/gcx/root/command.go`

Do NOT modify types.go, client.go, adapter.go, or resource_adapter.go.
Those are owned by Build-Core.

## CRITICAL: Response Envelope Shapes

Do NOT infer response envelope shapes. Copy deserialization code verbatim from
the grafana-cloud-cli source. If the source does `json.Unmarshal(body, &slice)`,
the new client MUST do the same — never wrap in a struct unless the source does.

## References

- **Spec**: {spec_path}/spec.md — functional requirements and acceptance criteria
- **Plan**: {spec_path}/plan.md — architecture decisions and HTTP client reference
- **Recipe**: `gcx-provider-recipe.md` Steps 6-8 (provider registration, CLI commands)
- **Commands Reference**: `commands-reference.md` — CRUD redirect patterns, codec usage

## Implementation Tasks

{paste assigned task excerpts from tasks.md here}

## Notes

- The adapter interfaces are already implemented by Build-Core. Import and use them;
  do not modify them.
- Before starting any command implementation that imports the adapter, confirm that
  the Build-Core task is marked complete.

## Completion

When all files are implemented and `make lint` passes on your files:
1. Send a message to the lead confirming completion and listing the files you created.
```
