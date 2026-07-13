# Confirmation and Safety

> Covers when to prompt users before destructive operations, the --force/GCX_AUTO_APPROVE pattern, dry-run support, and push idempotency.

---

## 3. Confirmation and Safety

### 3.1 When to Prompt

Prompt the user before:
- Deleting remote resources (single or bulk)
- Bulk overwrite operations (`push --overwrite` on an existing resource set)

Do NOT prompt for:
- Push (create-or-update) — it's idempotent
- Pull (local write) — easily reversible via git
- Config changes — low-risk, undoable

### 3.2 The `--force` Flag and `providers.ConfirmDestructive` `[IMPLEMENTED]`

All destructive provider commands use the shared `providers.ConfirmDestructive()`
helper. It applies a 3-layer bypass chain before falling through to an
interactive prompt:

1. **`--force` flag** — explicit per-invocation bypass
2. **`GCX_AUTO_APPROVE` env var** — enables non-interactive operation in CI/CD
3. **Agent mode without `--force`** — fails with actionable error
4. **Interactive prompt** — asks the user to confirm (`[y/N]`)

If none of the bypass conditions are met and stdin is closed/empty, the prompt's
`ReadString` returns EOF, surfacing a clear error.

```go
proceed, err := providers.ConfirmDestructive(
    cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
    fmt.Sprintf("Delete %d resource(s)?", count))
if err != nil {
    return err
}
if !proceed {
    return nil
}
```

**Convention:** Use `--force` (long flag only, no `-f` shorthand per
[naming.md](naming.md) § 9.4). Do not use `--yes`, `--skip-confirmations`,
or other variants.

**Note:** Auto-approval does NOT enable `--include-managed` to protect resources
managed by external tools (Terraform, GitSync, etc.). Users must explicitly pass
`--include-managed` if needed.

The `resources delete` command additionally supports `--yes` (`-y`) which
auto-enables the `--force` flag. This is a legacy pattern specific to the
resources layer; new provider commands should use `--force` directly.

### 3.3 Agent Mode Requires Explicit `--force` `[IMPLEMENTED]`

When agent mode is active ([agent-mode.md](agent-mode.md)), `providers.ConfirmDestructive`
**rejects** the operation with an actionable error unless `--force` is passed.
This forces agents to deliberately acknowledge destructive operations rather
than silently proceeding — creating an explicit audit trail and preventing
rogue agents from accidentally deleting resources.

### 3.4 Dry-Run

`--dry-run` is available on `push`, `delete`, and (implicitly) `validate`. It passes
`DryRun: []string{"All"}` to Kubernetes API options. Always document dry-run
support in new commands that modify remote state.

**Fail-safe guard.** Some Grafana APIs (all alerting resources today) ignore
server-side `dryRun` and apply the mutation anyway. A client-side guard
(`internal/resources/remote/dryrun_guard.go`) wraps the dynamic client and, for a
mutating dry-run against a resource **not** on the allowlist
(`dryrun_allowlist.go` — dashboards, folders, playlists), refuses to send the
request, warns on stderr, and records the resource as **skipped** (not pushed, not
failed; skips keep exit 0). Best-effort only — it confirms the operation is
well-formed and (for delete) that the target exists, not spec correctness ("not
verified"). Users who know a stack runs a resource on dual-write/unified can add it
via `--assume-server-dry-run <resource>.<group>` or the per-context
`resources.assume-server-dry-run` config. Robust long-term fix is server-side
(grafana-enterprise#12569).

### 3.5 Push Idempotency

Push is **idempotent** (create-or-update). The flow: Get → if exists: Update
with `resourceVersion`, if 404: Create. Safe to run repeatedly with the same
input. Document this explicitly in push-like commands:

```
# Push is idempotent: creates new resources and updates existing ones
gcx resources push ./dashboards/
```

Reference: `data-flows.md` Section 2 (PUSH Pipeline)
