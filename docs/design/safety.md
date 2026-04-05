# Confirmation and Safety

> Covers when to prompt users before destructive operations, the --yes/GCX_AUTO_APPROVE pattern, dry-run support, and push idempotency.
> Status markers: **[CURRENT]** = enforced, **[ADOPT]** = new code must follow, **[PLANNED]** = future.

---

## 3. Confirmation and Safety

### 3.1 When to Prompt `[ADOPT]`

Prompt the user before:
- Deleting remote resources (single or bulk)
- Bulk overwrite operations (`push --overwrite` on an existing resource set)

Do NOT prompt for:
- Push (create-or-update) — it's idempotent
- Pull (local write) — easily reversible via git
- Config changes — low-risk, undoable

### 3.2 The `--yes` / `-y` Pattern `[IMPLEMENTED]`

The `--yes`/`-y` flag and `GCX_AUTO_APPROVE` environment variable enable
non-interactive operation for destructive commands. Currently implemented for:

- **delete command**: Auto-enables `--force` flag (required to delete all resources of a type)

**Note:** Auto-approval does NOT enable `--include-managed` to protect resources
managed by external tools (Terraform, GitSync, etc.). Users must explicitly pass
`--include-managed` if needed.

Pattern (as implemented in `cmd/gcx/resources/delete.go`):

```go
// Load CLI options from environment
cliOpts, err := config.LoadCLIOptions()
if err != nil {
    return err
}

// Apply auto-approval logic
if (opts.Yes || cliOpts.AutoApprove) && !opts.Force {
    cmdio.Info(cmd.OutOrStdout(), "Auto-approval enabled: automatically setting --force")
    opts.Force = true
}
```

**Flag precedence:** Explicit flag value > --yes flag > env var > default

### 3.3 Agent Mode Auto-Approve `[PLANNED]`

When agent mode is active ([agent-mode.md](agent-mode.md)), prompts are auto-approved. Agents
cannot interact with TTY prompts.

### 3.4 Dry-Run `[CURRENT]`

`--dry-run` is available on `push` and `delete`. It passes
`DryRun: []string{"All"}` to Kubernetes API options. Always document dry-run
support in new commands that modify remote state.

### 3.5 Push Idempotency `[CURRENT]`

Push is **idempotent** (create-or-update). The flow: Get → if exists: Update
with `resourceVersion`, if 404: Create. Safe to run repeatedly with the same
input. Document this explicitly in push-like commands:

```
# Push is idempotent: creates new resources and updates existing ones
gcx resources push ./dashboards/
```

Reference: `data-flows.md` Section 2 (PUSH Pipeline)
