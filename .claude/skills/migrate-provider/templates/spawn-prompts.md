# Spawn Prompt Templates

Templates for the lead orchestrator to construct spawn prompts for Build and
Verify agents. Paste the completed audit artifacts into the marked sections.

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

## Build Envelope

### Parity Table

{paste the completed parity table here}

### Architectural Mapping

{paste the completed architectural mapping here}

### Recipe Reference

Follow `provider-migration-recipe.md` Steps 2-5 (types, client, adapter, resource_adapter)
for mechanical implementation steps. The recipe is authoritative for file structure,
client pattern, adapter wiring, and registration patterns.

## Completion

When all files are implemented and `make lint` passes on your files:
1. Mark the Build-Core task complete via TaskList.
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
- `cmd/grafanactl/providers/{name}/commands.go`
- `cmd/grafanactl/providers/{name}/*_test.go` (command tests)
- The blank import line in `cmd/grafanactl/root/command.go`

Do NOT modify types.go, client.go, adapter.go, or resource_adapter.go.
Those are owned by Build-Core.

## Build Envelope

### Parity Table

{paste the completed parity table here}

### Architectural Mapping

{paste the completed architectural mapping here}

### Recipe Reference

Follow `provider-migration-recipe.md` Steps 6-8 (provider registration, CLI commands)
for mechanical implementation steps. The recipe is authoritative for command
patterns, Options structs, and codec usage.

## Notes

- The adapter interfaces are already implemented by Build-Core. Import and use them;
  do not modify them.
- Before starting any command implementation that imports the adapter, confirm that
  the Build-Core task is marked complete in TaskList.

## Completion

When all files are implemented and `make lint` passes on your files:
1. Send a message to the lead confirming completion and listing the files you created.
```

---

## Verify Spawn Prompt

```
You are the Verify agent for the {provider} provider migration.

## Your Task

Execute the verification plan below and produce a structured comparison report.

You have access ONLY to the verification plan in this prompt. Do NOT reference any
Build-stage implementation details (internal function names, error handling approach,
test structure chosen by the builder). Derive all expected behavior from the
verification plan.

## Verify Envelope

### Verification Plan

{paste the completed verification plan here -- test list, smoke commands, pass criteria}

## Deliverables

1. **Comparison report** -- fill in the template from `templates/comparison-report.md`
   and present it to the user.

2. **Recipe update (REQUIRED -- FR-011)** -- after the report is complete, you
   MUST update `provider-migration-recipe.md` with:
   - **Status tracker entry** for this provider (required even if no issues found)
   - **Gotchas** (problems discovered during smoke tests; write "No new gotchas" if none)
   - Pattern corrections (if any recipe step was unclear or incorrect)

Execute every item in the verification plan. Do not skip or abbreviate any step.
```
